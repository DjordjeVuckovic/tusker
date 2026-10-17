package runner

import (
	"context"
	"errors"
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

var errEngineDown = errors.New("engine down")

// failingCallExecutor fails only its failOnCall-th Execute, counting from 1.
type failingCallExecutor struct {
	name       string
	failOnCall int
	calls      int
}

func (e *failingCallExecutor) Execute(context.Context, engine.Request) (*engine.Execution, error) {
	e.calls++
	if e.calls == e.failOnCall {
		return nil, errEngineDown
	}
	return &engine.Execution{Latency: time.Millisecond}, nil
}

func (e *failingCallExecutor) Name() string { return e.name }
func (e *failingCallExecutor) Close() error { return nil }

func TestRunAll_FailedIterations(t *testing.T) {
	tests := []struct {
		name       string
		warmup     int
		failOnCall int
		wantErr    error
	}{
		{name: "a failed measured iteration fails the query", failOnCall: 2, wantErr: errEngineDown},
		{name: "a failed warmup does not", warmup: 1, failOnCall: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bs := oneJobTrack(t, `queries:
  - id: q1
    engines:
      pg: { query: "SELECT id FROM articles" }
`, map[string]spec.Engine{"pg": postgres}, nil)
			cfg := DefaultConfig()
			cfg.WarmupRuns = tt.warmup
			cfg.Runs = 3

			result, err := New(cfg).RunAll(context.Background(), bs, map[string]engine.Executor{
				"pg": &failingCallExecutor{name: "pg", failOnCall: tt.failOnCall},
			})

			require.NoError(t, err)
			got := result.Jobs[0].Results["q1"]["pg"].Error
			if tt.wantErr == nil {
				assert.NoError(t, got)
				return
			}
			assert.ErrorIs(t, got, tt.wantErr)
		})
	}
}

func TestRunAll_EngineWithoutABlockForAQuery(t *testing.T) {
	const suiteYAML = `queries:
  - id: q-both
    engines:
      pg: { query: "SELECT id FROM articles" }
      es: { query: '{"query": {"match_all": {}}}' }
  - id: q-pg-only
    engines:
      pg: { query: "SELECT id FROM articles" }
`
	tests := []struct {
		name    string
		engines map[string]spec.Engine
		wantErr []string
	}{
		{
			name:    "fails before any query runs, naming the query and the engine",
			engines: map[string]spec.Engine{"pg": postgres, "es": elasticsearch},
			wantErr: []string{"q-pg-only", `"es"`},
		},
		{
			name: "an engine reusing another's blocks has every block",
			engines: map[string]spec.Engine{
				"pg":       postgres,
				"pg-tuned": {Type: spec.EnginePostgres, QueriesFrom: "pg"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bs := oneJobTrack(t, suiteYAML, tt.engines, nil)
			executors := map[string]engine.Executor{}
			recorders := map[string]*recordingExecutor{}
			for name := range tt.engines {
				recorders[name] = &recordingExecutor{name: name}
				executors[name] = templateStoringRecorder{recorders[name]}
			}

			_, err := New(DefaultConfig()).RunAll(context.Background(), bs, executors)

			if len(tt.wantErr) == 0 {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			for _, want := range tt.wantErr {
				assert.ErrorContains(t, err, want)
			}
			for name, rec := range recorders {
				assert.Empty(t, rec.seen, "engine %s ran a query before the job was rejected", name)
			}
		})
	}
}

// templateStoringRecorder lets a recording executor stand in for an engine type
// that stores search templates.
type templateStoringRecorder struct{ *recordingExecutor }

func (templateStoringRecorder) RegisterSearchTemplate(context.Context, engine.SearchTemplate) error {
	return nil
}
