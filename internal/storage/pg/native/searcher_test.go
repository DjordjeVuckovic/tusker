package native

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/storage/pg"
	"github.com/DjordjeVuckovic/tusker/internal/types/document"
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

func TestSearcher_ScoreNormalizedStaysInUnitRangeWhenEveryRankIsZero(t *testing.T) {
	s := newSearcher(t)
	articles, _ := pkgtesting.SearchContractCorpus()
	indexer, err := pg.NewIndexer(testPool)
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	if err := indexer.SaveBulk(testCtx, articles); err != nil {
		t.Fatalf("SaveBulk: %v", err)
	}

	// A field no weight band covers ranks every match at zero.
	res, err := s.SearchField(testCtx, dquery.NewMatch("no_such_field", "budget"), &dquery.BaseOptions{Size: 10})
	if err != nil {
		t.Fatalf("SearchField: %v", err)
	}
	for _, hit := range res.Hits {
		if math.IsNaN(hit.ScoreNormalized) || hit.ScoreNormalized < 0 || hit.ScoreNormalized > 1 {
			t.Errorf("article %s: ScoreNormalized = %v, want it in [0, 1]", hit.Article.ID, hit.ScoreNormalized)
		}
	}
}

func TestSearcher_CursorPastEveryMatchReturnsEmptyPage(t *testing.T) {
	s := newSearcher(t)
	indexer, err := pg.NewIndexer(testPool)
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	articles := []document.Article{
		{ID: uuid.New(), Title: "climate change policy", URL: "https://example.com/1"},
		{ID: uuid.New(), Title: "climate change", URL: "https://example.com/2"},
	}
	if err := indexer.SaveBulk(testCtx, articles); err != nil {
		t.Fatalf("SaveBulk: %v", err)
	}

	// Every match ranks above 0, so no row sorts after this cursor while the
	// match count stays positive.
	pastEveryMatch := &dquery.Cursor{Score: 0, ID: uuid.MustParse("00000000-0000-0000-0000-000000000001")}
	opts := &dquery.BaseOptions{Size: 10, Cursor: pastEveryMatch}

	methods := []struct {
		name   string
		search func() (*storage.SearchResult, error)
	}{
		{"SearchStringQuery", func() (*storage.SearchResult, error) {
			return s.SearchStringQuery(testCtx, dquery.NewQueryString("climate change"), opts)
		}},
		{"SearchField", func() (*storage.SearchResult, error) {
			return s.SearchField(testCtx, dquery.NewMatch("title", "climate change"), opts)
		}},
		{"SearchFields", func() (*storage.SearchResult, error) {
			q, err := dquery.NewMultiMatchQuery("climate change", []string{"title", "content"})
			if err != nil {
				return nil, err
			}
			return s.SearchFields(testCtx, q, opts)
		}},
		{"SearchPhrase exact", func() (*storage.SearchResult, error) {
			q, err := dquery.NewPhrase("climate change", []string{"title"})
			if err != nil {
				return nil, err
			}
			return s.SearchPhrase(testCtx, q, opts)
		}},
		{"SearchPhrase with slop", func() (*storage.SearchResult, error) {
			q, err := dquery.NewPhrase("climate policy", []string{"title"}, dquery.WithPhraseSlop(2))
			if err != nil {
				return nil, err
			}
			return s.SearchPhrase(testCtx, q, opts)
		}},
		{"SearchBoolean", func() (*storage.SearchResult, error) {
			return s.SearchBoolean(testCtx, &dquery.Boolean{Expression: "climate AND change"}, opts)
		}},
	}

	for _, method := range methods {
		t.Run(method.name, func(t *testing.T) {
			res, err := method.search()
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			if len(res.Hits) != 0 {
				t.Errorf("hits = %d, want none past the last match", len(res.Hits))
			}
			if res.HasMore || res.NextCursor != nil {
				t.Errorf("HasMore = %v, NextCursor = %+v, want no further page", res.HasMore, res.NextCursor)
			}
		})
	}
}

func TestSearcher_PagesThroughEveryHit(t *testing.T) {
	s := newSearcher(t)
	indexer, err := pg.NewIndexer(testPool)
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	var matching []uuid.UUID
	for i, title := range []string{"climate", "climate change", "climate change policy", "climate change policy debate review", "unrelated cooking recipe"} {
		id := uuid.New()
		if err := indexer.SaveBulk(testCtx, []document.Article{{ID: id, Title: title, URL: fmt.Sprintf("https://example.com/%d", i)}}); err != nil {
			t.Fatalf("SaveBulk: %v", err)
		}
		if strings.Contains(title, "climate") {
			matching = append(matching, id)
		}
	}

	methods := []struct {
		name   string
		search func(opts *dquery.BaseOptions) (*storage.SearchResult, error)
	}{
		{"SearchStringQuery", func(opts *dquery.BaseOptions) (*storage.SearchResult, error) {
			return s.SearchStringQuery(testCtx, dquery.NewQueryString("climate"), opts)
		}},
		{"SearchField", func(opts *dquery.BaseOptions) (*storage.SearchResult, error) {
			return s.SearchField(testCtx, dquery.NewMatch("title", "climate"), opts)
		}},
		{"SearchFields", func(opts *dquery.BaseOptions) (*storage.SearchResult, error) {
			q, err := dquery.NewMultiMatchQuery("climate", []string{"title", "content"})
			if err != nil {
				return nil, err
			}
			return s.SearchFields(testCtx, q, opts)
		}},
		{"SearchPhrase", func(opts *dquery.BaseOptions) (*storage.SearchResult, error) {
			q, err := dquery.NewPhrase("climate", []string{"title"})
			if err != nil {
				return nil, err
			}
			return s.SearchPhrase(testCtx, q, opts)
		}},
		{"SearchBoolean", func(opts *dquery.BaseOptions) (*storage.SearchResult, error) {
			return s.SearchBoolean(testCtx, &dquery.Boolean{Expression: "climate"}, opts)
		}},
	}

	const pageSize = 2
	for _, method := range methods {
		t.Run(method.name, func(t *testing.T) {
			seen := map[uuid.UUID]bool{}
			opts := &dquery.BaseOptions{Size: pageSize}
			for page := 1; ; page++ {
				if page > len(matching)+2 {
					t.Fatalf("still paging after %d pages", page-1)
				}
				res, err := method.search(opts)
				if err != nil {
					t.Fatalf("page %d: %v", page, err)
				}
				if len(res.Hits) == 0 {
					t.Fatalf("page %d is empty", page)
				}
				for _, hit := range res.Hits {
					if seen[hit.Article.ID] {
						t.Errorf("page %d repeats article %s", page, hit.Article.ID)
					}
					seen[hit.Article.ID] = true
				}
				if !res.HasMore {
					if res.NextCursor != nil {
						t.Errorf("last page carries NextCursor %+v", res.NextCursor)
					}
					break
				}
				if res.NextCursor == nil {
					t.Fatalf("page %d: HasMore without a NextCursor", page)
				}
				opts = &dquery.BaseOptions{Size: pageSize, Cursor: res.NextCursor}
			}
			for _, id := range matching {
				if !seen[id] {
					t.Errorf("matching article %s never returned", id)
				}
			}
		})
	}
}
