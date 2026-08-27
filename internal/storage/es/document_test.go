package es

import (
	"context"
	"errors"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/storage"
	pkgtesting "github.com/DjordjeVuckovic/tusker/pkg/testing"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/densevectorsimilarity"
)

func TestIndexBuilder_DenseVectorDeclaresHNSWBuildParams(t *testing.T) {
	p := NewIndexBuilder().denseVectorProperty()

	if p.IndexOptions == nil {
		t.Fatal("embedding field left index_options unset, so Elasticsearch builds at its own default")
	}
	if got := p.IndexOptions.Type.String(); got != "hnsw" {
		t.Errorf("index_options.type = %q, want hnsw", got)
	}
	if p.IndexOptions.M == nil || *p.IndexOptions.M != storage.HNSWM {
		t.Errorf("index_options.m = %v, want %d", p.IndexOptions.M, storage.HNSWM)
	}
	if p.IndexOptions.EfConstruction == nil || *p.IndexOptions.EfConstruction != storage.HNSWEfConstruction {
		t.Errorf("index_options.ef_construction = %v, want %d", p.IndexOptions.EfConstruction, storage.HNSWEfConstruction)
	}
}

// TestEnsureIndex_MappingReportsHNSWBuildParams reads the parameters back out of
// Elasticsearch. An index whose index_options is left unset reports nothing here
// — declaring the values is what makes them auditable after a run.
func TestEnsureIndex_MappingReportsHNSWBuildParams(t *testing.T) {
	ctx := context.Background()
	container := pkgtesting.NewESContainer(ctx, t)
	cfg := ClientConfig{Addresses: []string{container.Address}, IndexName: "articles_hnsw_test"}

	if _, err := NewIndexer(ctx, cfg); err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}

	client, err := newClient(cfg)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	assertDeclaredBuildParams(t, client, cfg.IndexName)
}

// TestEmbedder_RejectsAnIndexBuiltAtOtherBuildParams covers the indices loaded
// before the parameters were declared: their embedding field carries no
// index_options, and Elasticsearch refuses to add one to a field that exists. A
// load into such an index would fill a graph nobody chose the build point of, so
// it has to stop and say what to do instead.
func TestEmbedder_RejectsAnIndexBuiltAtOtherBuildParams(t *testing.T) {
	ctx := context.Background()
	container := pkgtesting.NewESContainer(ctx, t)
	cfg := ClientConfig{Addresses: []string{container.Address}, IndexName: "articles_hnsw_legacy_test"}

	client, err := newClient(cfg)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	createIndexWithDefaultedVectorField(t, client, cfg.IndexName)

	_, err = NewEmbedder(ctx, cfg)
	if err == nil {
		t.Fatal("NewEmbedder accepted an index built at the elasticsearch default parameters")
	}
	var mismatch *VectorIndexMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error is %v, want a *VectorIndexMismatchError naming the rebuild", err)
	}
	if mismatch.Index != cfg.IndexName {
		t.Errorf("mismatch reports index %q, want %q", mismatch.Index, cfg.IndexName)
	}
}

// createIndexWithDefaultedVectorField builds the embedding field the way it was
// shaped before the build parameters were declared: indexed and cosine, with
// index_options left to Elasticsearch.
func createIndexWithDefaultedVectorField(t *testing.T, client *elasticsearch.TypedClient, index string) {
	t.Helper()

	dims := EmbeddingDims
	indexed := true
	sim := densevectorsimilarity.Cosine
	vector := types.NewDenseVectorProperty()
	vector.Dims = &dims
	vector.Index = &indexed
	vector.Similarity = &sim

	mappings := types.TypeMapping{
		Properties: map[string]types.Property{
			"id":        types.NewKeywordProperty(),
			"embedding": vector,
		},
	}
	if _, err := client.Indices.Create(index).Mappings(&mappings).Do(context.Background()); err != nil {
		t.Fatalf("create index with a defaulted vector field: %v", err)
	}
}

func assertDeclaredBuildParams(t *testing.T, client *elasticsearch.TypedClient, index string) {
	t.Helper()

	res, err := client.Indices.GetMapping().Index(index).Do(context.Background())
	if err != nil {
		t.Fatalf("get mapping: %v", err)
	}
	record, ok := res[index]
	if !ok {
		t.Fatalf("no mapping returned for index %q", index)
	}
	vector, ok := record.Mappings.Properties["embedding"].(*types.DenseVectorProperty)
	if !ok {
		t.Fatalf("embedding property is %T, want *types.DenseVectorProperty", record.Mappings.Properties["embedding"])
	}
	if vector.IndexOptions == nil {
		t.Fatal("elasticsearch reports no index_options for the embedding field")
	}
	if vector.IndexOptions.M == nil || *vector.IndexOptions.M != storage.HNSWM {
		t.Errorf("index_options.m = %v, want %d", vector.IndexOptions.M, storage.HNSWM)
	}
	if vector.IndexOptions.EfConstruction == nil || *vector.IndexOptions.EfConstruction != storage.HNSWEfConstruction {
		t.Errorf("index_options.ef_construction = %v, want %d", vector.IndexOptions.EfConstruction, storage.HNSWEfConstruction)
	}
}
