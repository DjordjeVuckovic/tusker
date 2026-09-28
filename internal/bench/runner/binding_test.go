package runner

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/bench/engine"
	"github.com/DjordjeVuckovic/tusker/internal/bench/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingExecutor struct {
	name string
	mu   sync.Mutex
	seen []string
}

func (e *recordingExecutor) Execute(_ context.Context, query string, _ []any) (*engine.Execution, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seen = append(e.seen, query)
	return &engine.Execution{}, nil
}

func (e *recordingExecutor) Name() string { return e.name }

func (e *recordingExecutor) Close() error { return nil }

func (e *recordingExecutor) lastQuery() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.seen) == 0 {
		return ""
	}
	return e.seen[len(e.seen)-1]
}

// An engine declaring queries_from runs the source engine's query with its own
// params: the A/B arms share one suite block and can only differ where the
// spec says they do.
func TestRunAll_AliasedEngineRunsSourceBlockWithOwnParams(t *testing.T) {
	dir := t.TempDir()
	suitePath := filepath.Join(dir, "suite.yaml")
	require.NoError(t, os.WriteFile(suitePath, []byte(`schema_version: 1
id: alias_suite
templates:
  - id: pg_idx
    query: "ORDER BY ts_rank(search_vector, q('{{terms}}'), {{rank_norm}}) DESC"
queries:
  - id: qs-climate
    engines:
      pg-gin:
        template: pg_idx
        params: { terms: "climate change" }
`), 0644))

	bs := &spec.BenchSpec{
		Engines: map[string]spec.Engine{
			"pg-gin":      {Params: map[string]any{"rank_norm": "0"}},
			"pg-gin-norm": {QueriesFrom: "pg-gin", Params: map[string]any{"rank_norm": "1"}},
		},
		Jobs: []spec.Job{{
			Name:    "rank-ab",
			Suite:   suitePath,
			Engines: []string{"pg-gin", "pg-gin-norm"},
		}},
	}

	gin := &recordingExecutor{name: "pg-gin"}
	norm := &recordingExecutor{name: "pg-gin-norm"}

	cfg := DefaultConfig()
	cfg.WarmupRuns = 0
	cfg.Runs = 1
	_, err := New(cfg).RunAll(context.Background(), bs, map[string]engine.Executor{
		"pg-gin":      gin,
		"pg-gin-norm": norm,
	})
	require.NoError(t, err)

	assert.Equal(t, "ORDER BY ts_rank(search_vector, q('climate change'), 0) DESC", gin.lastQuery())
	assert.Equal(t, "ORDER BY ts_rank(search_vector, q('climate change'), 1) DESC", norm.lastQuery())
}
