package factory

import (
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/storage/es"
	"github.com/DjordjeVuckovic/tusker/internal/storage/pg"
)

func TestVectorSearchers_RequireEmbedder(t *testing.T) {
	esClient, err := es.NewClient(es.ClientConfig{Addresses: []string{"http://127.0.0.1:1"}, IndexName: "articles"})
	if err != nil {
		t.Fatalf("es.NewClient: %v", err)
	}
	backends := []struct {
		name string
		cfg  SearcherConfig
	}{
		{name: "postgres", cfg: SearcherConfig{Type: storage.PG, Pool: &pg.ConnectionPool{}}},
		{name: "elasticsearch", cfg: SearcherConfig{Type: storage.ES, EsClient: esClient}},
	}

	for _, backend := range backends {
		t.Run(backend.name+"/semantic", func(t *testing.T) {
			if _, err := NewSemanticSearcher(backend.cfg); err == nil {
				t.Error("NewSemanticSearcher without an embedder succeeded, want an error")
			}
		})
		t.Run(backend.name+"/hybrid", func(t *testing.T) {
			if _, err := NewHybridSearcher(backend.cfg); err == nil {
				t.Error("NewHybridSearcher without an embedder succeeded, want an error")
			}
		})
	}
}
