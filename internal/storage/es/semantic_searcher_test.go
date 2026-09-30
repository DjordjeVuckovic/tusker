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

// staticEmbedder is an embedding.Client returning a fixed vector.
type staticEmbedder struct {
	vec []float32
}

func (s staticEmbedder) Generate(_ context.Context, _ embedding.Request) (*embedding.Response, error) {
	return &embedding.Response{Embedding: s.vec}, nil
}

func (s staticEmbedder) GenerateBatch(_ context.Context, _ embedding.BatchRequest) (*embedding.BatchResponse, error) {
	return &embedding.BatchResponse{Embeddings: [][]float32{s.vec}}, nil
}

func newSemanticTestEnv(t *testing.T, queryVec []float32) (*Indexer, *Embedder, *SemanticSearcher) {
	t.Helper()

	container := pkgtesting.NewESContainer(context.Background(), t)
	cfg := ClientConfig{Addresses: []string{container.Address}, IndexName: "articles_semantic_test"}

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
	searcher, err := NewSemanticSearcher(cfg, qEmbedder, embedding.DefaultModel)
	if err != nil {
		t.Fatalf("NewSemanticSearcher: %v", err)
	}
	return indexer, embIndexer, searcher
}

func TestSemanticSearcher_SearchSemantic_ReturnsNearest(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Docker (testcontainers ES)")
	}
	ctx := context.Background()

	near := vec(EmbeddingDims, 0.9)
	far := vec(EmbeddingDims, -0.9)

	indexer, embIndexer, searcher := newSemanticTestEnv(t, near)

	nearID := uuid.New()
	farID := uuid.New()
	if _, err := indexer.Save(ctx, document.Article{ID: nearID, Title: "near", Language: "english"}); err != nil {
		t.Fatalf("index near article: %v", err)
	}
	if _, err := indexer.Save(ctx, document.Article{ID: farID, Title: "far", Language: "english"}); err != nil {
		t.Fatalf("index far article: %v", err)
	}
	refresh(t, embIndexer)

	batch := []*embedding.Vec{
		{ID: nearID, Model: embedding.DefaultModel, Embedding: near},
		{ID: farID, Model: embedding.DefaultModel, Embedding: far},
	}
	if _, err := embIndexer.SaveBulk(ctx, batch); err != nil {
		t.Fatalf("SaveBulk: %v", err)
	}
	refresh(t, embIndexer)

	res, err := searcher.SearchSemantic(ctx, &dquery.Semantic{Query: "anything"}, &dquery.BaseOptions{Size: 1})
	if err != nil {
		t.Fatalf("SearchSemantic: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Fatal("expected at least one hit")
	}
	if res.Hits[0].ID != nearID {
		t.Errorf("nearest hit = %s, want %s", res.Hits[0].ID, nearID)
	}
}

func TestSemanticSearcher_SearchSemantic_OnlySeesTheQueryModel(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Docker (testcontainers ES)")
	}
	ctx := context.Background()

	queryVec := vec(EmbeddingDims, 0.9)
	nearQuery := vec(EmbeddingDims, 0.9)
	for i := len(nearQuery) / 2; i < len(nearQuery); i++ {
		nearQuery[i] = 0.5
	}

	indexer, embIndexer, searcher := newSemanticTestEnv(t, queryVec)

	sameModel := uuid.New()
	otherModel := uuid.New()
	for _, id := range []uuid.UUID{sameModel, otherModel} {
		if _, err := indexer.Save(ctx, document.Article{ID: id, Title: id.String(), Language: "english"}); err != nil {
			t.Fatalf("index article: %v", err)
		}
	}
	refresh(t, embIndexer)

	batch := []*embedding.Vec{
		{ID: sameModel, Model: embedding.DefaultModel, Embedding: nearQuery},
		{ID: otherModel, Model: "other-model", Embedding: queryVec},
	}
	if _, err := embIndexer.SaveBulk(ctx, batch); err != nil {
		t.Fatalf("SaveBulk: %v", err)
	}
	refresh(t, embIndexer)

	res, err := searcher.SearchSemantic(ctx, &dquery.Semantic{Query: "anything"}, &dquery.BaseOptions{Size: 1})
	if err != nil {
		t.Fatalf("SearchSemantic: %v", err)
	}

	got := make([]uuid.UUID, 0, len(res.Hits))
	for _, hit := range res.Hits {
		got = append(got, hit.ID)
	}
	if len(got) != 1 || got[0] != sameModel {
		t.Errorf("hits = %v, want only %s: a vector from another model must not be ranked, nor shrink k", got, sameModel)
	}
}
