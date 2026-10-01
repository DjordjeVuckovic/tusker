package report

import (
	"path/filepath"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/bench/engine"
	"github.com/DjordjeVuckovic/tusker/internal/bench/metrics"
	"github.com/DjordjeVuckovic/tusker/internal/bench/spec"
	"github.com/DjordjeVuckovic/tusker/internal/storage/es"
	"github.com/DjordjeVuckovic/tusker/internal/storage/pg"
)

func TestGenerate_RecordsIndexProvenanceThroughTheWrittenReport(t *testing.T) {
	br := makeBenchmarkResult([]string{"pg", "es"}, []string{"q1"}, map[string]map[string]metrics.ScoreSet{
		"q1": {"pg": makeJudgedScores(0.7), "es": makeJudgedScores(0.5)},
	})
	rpt := Generate(br, &GenerateOptions{
		Spec: &spec.BenchSpec{Engines: map[string]spec.Engine{
			"pg":  {Type: "postgres"},
			"es":  {Type: "elasticsearch"},
			"api": {Type: "api"},
		}},
		Indexes: map[string]IndexProvenance{
			"pg": {IndexDescription: engine.IndexDescription{Postgres: &pg.IndexDescription{
				ServerVersion: "18.0",
				Settings:      map[string]string{"hnsw.ef_search": "200"},
			}}},
			"es": {IndexDescription: engine.IndexDescription{Elasticsearch: &es.IndexDescription{
				Version: "8.15.0",
				Index:   "news-cc-000001",
			}}},
			"api": {Error: "engine cannot describe its index"},
		},
	})

	path := filepath.Join(t.TempDir(), "report.json")
	if err := WriteJSON(rpt, path); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	got, err := ReadJSON(path)
	if err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}

	engines := got.Environment.Engines
	if v := engines["pg"].Version; v != "18.0" {
		t.Errorf("pg version = %q, want 18.0", v)
	}
	if v := engines["es"].Version; v != "8.15.0" {
		t.Errorf("es version = %q, want 8.15.0", v)
	}
	if idx := engines["pg"].Index; idx == nil || idx.Postgres == nil || idx.Postgres.Settings["hnsw.ef_search"] != "200" {
		t.Errorf("pg index provenance = %+v, want the ef_search it was measured at", idx)
	}
	if idx := engines["es"].Index; idx == nil || idx.Elasticsearch == nil || idx.Elasticsearch.Index != "news-cc-000001" {
		t.Errorf("es index provenance = %+v, want the concrete index", idx)
	}
	if idx := engines["api"].Index; idx == nil || idx.Error == "" {
		t.Errorf("api index provenance = %+v, want a block carrying the error", idx)
	}
}
