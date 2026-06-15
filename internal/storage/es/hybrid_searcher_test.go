package es

import (
	"context"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/embedding"
	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	dquery "github.com/DjordjeVuckovic/tusker/internal/types/query"
	pkgtesting "github.com/DjordjeVuckovic/tusker/pkg/testing"
	"github.com/google/uuid"
)

func newHybridTestEnv(t *testing.T, queryVec []float32) (*Indexer, *Embedder, *HybridSearcher) {
	t.Helper()

	container := pkgtesting.NewESContainer(context.Background(), t)
	cfg := ClientConfig{Addresses: []string{container.Address}, IndexName: "articles_hybrid_test"}

	indexer, err := NewIndexer(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	embIndexer, err := NewEmbedder(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewEmbedder: %v", err)
	}

	qEmbedder := embedding.NewEmbedder(staticEmbedder{vec: queryVec},
		embedding.WithExecutorMaxLength(1024),
		embedding.WithExecutorModel(embedding.DefaultModel),
	)
	searcher, err := NewHybridSearcher(cfg, qEmbedder, embedding.DefaultModel)
	if err != nil {
		t.Fatalf("NewHybridSearcher: %v", err)
	}
	return indexer, embIndexer, searcher
}

func TestHybridSearcher_SearchHybrid_RanksBothSignalsFirst(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Docker (testcontainers ES)")
	}
	ctx := context.Background()

	near := vec(EmbeddingDims, 0.9)
	far := vec(EmbeddingDims, -0.9)

	indexer, embIndexer, searcher := newHybridTestEnv(t, near)

	// bothID: strong lexical match AND near query vector.
	// lexicalOnlyID: lexical match but far vector.
	// vectorOnlyID: near vector but no lexical match.
	bothID := uuid.New()
	lexicalOnlyID := uuid.New()
	vectorOnlyID := uuid.New()

	if _, err := indexer.Save(ctx, document.Article{ID: bothID, Title: "climate change policy", Language: "english"}); err != nil {
		t.Fatalf("index both article: %v", err)
	}
	if _, err := indexer.Save(ctx, document.Article{ID: lexicalOnlyID, Title: "climate change report", Language: "english"}); err != nil {
		t.Fatalf("index lexical article: %v", err)
	}
	if _, err := indexer.Save(ctx, document.Article{ID: vectorOnlyID, Title: "unrelated cooking recipe", Language: "english"}); err != nil {
		t.Fatalf("index vector article: %v", err)
	}
	refresh(t, embIndexer)

	batch := []*embedding.Vec{
		{ID: bothID, Model: embedding.DefaultModel, Embedding: near},
		{ID: lexicalOnlyID, Model: embedding.DefaultModel, Embedding: far},
		{ID: vectorOnlyID, Model: embedding.DefaultModel, Embedding: near},
	}
	if err := embIndexer.SaveBulk(ctx, batch); err != nil {
		t.Fatalf("SaveBulk: %v", err)
	}
	refresh(t, embIndexer)

	res, err := searcher.SearchHybrid(ctx,
		dquery.NewHybrid("climate change"),
		&dquery.BaseOptions{Size: 10},
	)
	if err != nil {
		t.Fatalf("SearchHybrid: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Fatal("expected at least one hit")
	}
	if res.Hits[0].Article.ID != bothID {
		t.Errorf("top hit = %s, want %s (strong on both signals)", res.Hits[0].Article.ID, bothID)
	}
	if res.HasMore {
		t.Error("hybrid must return a single page (HasMore=false)")
	}
	if res.NextCursor != nil {
		t.Error("hybrid must not return a cursor")
	}
	if res.TotalMatches != int64(len(res.Hits)) {
		t.Errorf("TotalMatches = %d, want %d", res.TotalMatches, len(res.Hits))
	}
	if res.MaxScore <= 0 {
		t.Errorf("MaxScore = %f, want > 0", res.MaxScore)
	}
}
