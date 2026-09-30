package native

import (
	"context"
	"flag"
	"os"
	"slices"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/storage/pg"
	dquery "github.com/DjordjeVuckovic/tusker/internal/types/query"
	pkgtesting "github.com/DjordjeVuckovic/tusker/pkg/testing"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
)

var (
	testCtx  context.Context
	testPool *pg.ConnectionPool
)

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	os.Exit(runWithContainer(m))
}

func runWithContainer(m *testing.M) int {
	testCtx = context.Background()
	container, err := pkgtesting.NewPGContainer(testCtx, pkgtesting.PGConfig{
		Database: "news_test_db",
		Username: "test",
		Password: "test",
	})
	if err != nil {
		panic(err)
	}
	defer func() { _ = testcontainers.TerminateContainer(container.Container) }()

	testPool, err = pg.NewConnectionPool(testCtx, pg.PoolConfig{ConnStr: container.ConnString})
	if err != nil {
		panic(err)
	}
	defer testPool.Close()

	return m.Run()
}

func newSearcher(t *testing.T) *Searcher {
	t.Helper()
	if testPool == nil {
		t.Skip("requires Docker (testcontainers Postgres)")
	}
	if _, err := testPool.GetConn().Exec(testCtx, "TRUNCATE TABLE articles CASCADE"); err != nil {
		t.Fatalf("truncate articles: %v", err)
	}
	searcher, err := NewReader(testPool)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	return searcher
}

func hitIDs(t *testing.T, s *Searcher, query *dquery.String) []uuid.UUID {
	t.Helper()
	res, err := s.SearchStringQuery(testCtx, query, &dquery.BaseOptions{Size: 100})
	if err != nil {
		t.Fatalf("SearchStringQuery(%q): %v", query.Query, err)
	}
	ids := make([]uuid.UUID, 0, len(res.Hits))
	for _, hit := range res.Hits {
		ids = append(ids, hit.Article.ID)
	}
	if int64(len(ids)) != res.TotalMatches {
		t.Errorf("TotalMatches = %d, but the page holds all %d hits", res.TotalMatches, len(ids))
	}
	return ids
}

func sortedIDs(ids []uuid.UUID) []uuid.UUID {
	out := slices.Clone(ids)
	slices.SortFunc(out, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	return out
}

func TestSearcher_StringQueryRecallFollowsSearchContract(t *testing.T) {
	s := newSearcher(t)
	articles, want := pkgtesting.SearchContractCorpus()
	indexer, err := pg.NewIndexer(testPool)
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	if err := indexer.SaveBulk(testCtx, articles); err != nil {
		t.Fatalf("SaveBulk: %v", err)
	}

	got := hitIDs(t, s, dquery.NewQueryString(pkgtesting.SearchContractQuery))

	if !slices.Equal(sortedIDs(got), sortedIDs(want)) {
		t.Errorf("recall = %v, want %v", sortedIDs(got), sortedIDs(want))
	}
}

func TestSearcher_StringQueryOfOnlyStopwordsReturnsNoHits(t *testing.T) {
	s := newSearcher(t)
	articles, _ := pkgtesting.SearchContractCorpus()
	indexer, err := pg.NewIndexer(testPool)
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	if err := indexer.SaveBulk(testCtx, articles); err != nil {
		t.Fatalf("SaveBulk: %v", err)
	}

	if got := hitIDs(t, s, dquery.NewQueryString("the")); len(got) != 0 {
		t.Errorf("hits = %v, want none", got)
	}
}
