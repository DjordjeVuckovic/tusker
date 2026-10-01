package native

import (
	"context"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/storage/pg"
	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	dquery "github.com/DjordjeVuckovic/tusker/internal/types/query"
	pkgtesting "github.com/DjordjeVuckovic/tusker/pkg/testing"
)

type searchMethod struct {
	name   string
	search func(ctx context.Context, s *Searcher, term string, opts *dquery.BaseOptions) (*storage.SearchResult, error)
}

var searchMethods = []searchMethod{
	{
		name: "SearchStringQuery",
		search: func(ctx context.Context, s *Searcher, term string, opts *dquery.BaseOptions) (*storage.SearchResult, error) {
			return s.SearchStringQuery(ctx, &dquery.String{Query: term}, opts)
		},
	},
	{
		name: "SearchField",
		search: func(ctx context.Context, s *Searcher, term string, opts *dquery.BaseOptions) (*storage.SearchResult, error) {
			return s.SearchField(ctx, dquery.NewMatch("title", term), opts)
		},
	},
	{
		name: "SearchFields",
		search: func(ctx context.Context, s *Searcher, term string, opts *dquery.BaseOptions) (*storage.SearchResult, error) {
			q, err := dquery.NewMultiMatchQuery(term, []string{"title", "content"})
			if err != nil {
				return nil, err
			}
			return s.SearchFields(ctx, q, opts)
		},
	},
	{
		name: "SearchPhrase",
		search: func(ctx context.Context, s *Searcher, term string, opts *dquery.BaseOptions) (*storage.SearchResult, error) {
			q, err := dquery.NewPhrase(term, []string{"title"})
			if err != nil {
				return nil, err
			}
			return s.SearchPhrase(ctx, q, opts)
		},
	},
	{
		name: "SearchBoolean",
		search: func(ctx context.Context, s *Searcher, term string, opts *dquery.BaseOptions) (*storage.SearchResult, error) {
			return s.SearchBoolean(ctx, &dquery.Boolean{Expression: term}, opts)
		},
	},
}

func TestSearcher_HitCarriesTheIndexedColumns(t *testing.T) {
	ctx := context.Background()
	container := pkgtesting.NewPGContainerWithCleanup(ctx, t)
	pool, err := pg.NewConnectionPool(ctx, pg.PoolConfig{ConnStr: container.ConnString})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	indexer, err := pg.NewIndexer(pool)
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	seeded := document.Article{
		Title:      "harbour lights",
		Content:    "harbour lights at night",
		URL:        "https://example.com/harbour",
		Language:   "english",
		SourceName: "bbc.com",
	}
	id, err := indexer.Save(ctx, seeded)
	if err != nil {
		t.Fatalf("save article: %v", err)
	}

	searcher, err := NewReader(pool)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	for _, method := range searchMethods {
		t.Run(method.name, func(t *testing.T) {
			res, err := method.search(ctx, searcher, "harbour", &dquery.BaseOptions{Size: 10})
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			if len(res.Hits) != 1 || res.Hits[0].ID != id {
				t.Fatalf("hits = %+v, want only article %s", res.Hits, id)
			}
			hit := res.Hits[0].Article
			if hit.SourceName != seeded.SourceName {
				t.Errorf("SourceName = %q, want %q", hit.SourceName, seeded.SourceName)
			}
			if hit.Language != seeded.Language {
				t.Errorf("Language = %q, want %q", hit.Language, seeded.Language)
			}
		})
	}
}
