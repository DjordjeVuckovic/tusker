package runner

import (
	"context"
	"testing"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/bench/engine"
	"github.com/DjordjeVuckovic/tusker/internal/bench/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stallingExecutor answers only after the context ends, or succeeds once
// noDeadlineFallback passes so a run with no deadline fails the test instead
// of hanging it.
type stallingExecutor struct{ name string }

const noDeadlineFallback = 5 * time.Second

func (e stallingExecutor) Execute(ctx context.Context, _ engine.Request) (*engine.Execution, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(noDeadlineFallback):
		return &engine.Execution{}, nil
	}
}

func (e stallingExecutor) Name() string { return e.name }
func (e stallingExecutor) Close() error { return nil }

func (e stallingExecutor) RegisterSearchTemplate(context.Context, engine.SearchTemplate) error {
	return nil
}

func TestRunAll_EveryEngineFailsAtTheSameQueryDeadline(t *testing.T) {
	bs := oneJobTrack(t, `queries:
  - id: q-slow
    engines:
      pg: { query: "SELECT id FROM articles" }
      es: { query: '{"query": {"match_all": {}}}' }
`, map[string]spec.Engine{"pg": postgres, "es": elasticsearch}, nil)
	bs.Runs.QueryTimeout = 50 * time.Millisecond

	cfg := DefaultConfig()
	cfg.WarmupRuns = 0
	cfg.Runs = 1
	result, err := New(cfg).RunAll(context.Background(), bs, map[string]engine.Executor{
		"pg": stallingExecutor{name: "pg"},
		"es": stallingExecutor{name: "es"},
	})

	require.NoError(t, err)
	for _, name := range []string{"pg", "es"} {
		assert.ErrorIs(t, result.Jobs[0].Results["q-slow"][name].Error, context.DeadlineExceeded, name)
	}
}

type stallingRegistrar struct{ stallingExecutor }

func (e stallingRegistrar) RegisterSearchTemplate(ctx context.Context, _ engine.SearchTemplate) error {
	_, err := e.Execute(ctx, engine.Request{})
	return err
}

func TestRunAll_StoringATemplateFailsAtTheQueryDeadline(t *testing.T) {
	bs := oneJobTrack(t, `templates:
  - id: es_match
    args: [terms]
    query: '{"query": {"match": {"title": "{{terms}}"}}}'
queries:
  - id: q1
    engines:
      es: { template: es_match, params: { terms: climate } }
`, map[string]spec.Engine{"es": elasticsearch}, nil)
	bs.Runs.QueryTimeout = 50 * time.Millisecond

	_, err := New(DefaultConfig()).RunAll(context.Background(), bs, map[string]engine.Executor{
		"es": stallingRegistrar{stallingExecutor{name: "es"}},
	})

	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
