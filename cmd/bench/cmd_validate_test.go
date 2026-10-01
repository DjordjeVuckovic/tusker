package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/bench/engine"
	"github.com/DjordjeVuckovic/tusker/internal/bench/spec"
	"github.com/DjordjeVuckovic/tusker/internal/bench/suite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An interrupt mid-validate must surface as the interrupt: validating the
// remaining pairs against a cancelled context records each one as a failure
// that never happened, and the glob would carry on to the next track.
func TestValidate_InterruptReportsCancellationNotFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	es := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cancel()
		_, _ = w.Write([]byte(`{"valid": true}`))
	}))
	defer es.Close()

	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		writeValidateTrack(t, filepath.Join(root, "g", name), es.URL)
	}
	prevRoot := trackRoot
	trackRoot = root
	t.Cleanup(func() { trackRoot = prevRoot })

	var out bytes.Buffer
	cmd := newValidateCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"g/*"})
	err := cmd.ExecuteContext(ctx)

	require.ErrorIs(t, err, context.Canceled)
	assert.NotContains(t, out.String(), "g/b")
	assert.NotContains(t, out.String(), "failed")
}

func writeValidateTrack(t *testing.T, dir, esURL string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "trec"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.yaml"), fmt.Appendf(nil, `schema_version: 1
id: validate_interrupt
engines:
  es:
    type: elasticsearch
    connection: %q
    index: articles
jobs:
  - name: es-only
    suite: suite.yaml
    engines: [es]
`, esURL), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "suite.yaml"), []byte(`schema_version: 1
id: validate_interrupt
queries:
  - id: q-climate
    engines:
      es: { query: '{"query": {"match": {"title": "climate"}}}' }
  - id: q-election
    engines:
      es: { query: '{"query": {"match": {"title": "election"}}}' }
`), 0o644))
}

type recordingValidator struct {
	query string
	args  []any
}

func (v *recordingValidator) Execute(context.Context, string, []any) (*engine.Execution, error) {
	return &engine.Execution{}, nil
}
func (v *recordingValidator) Name() string { return "recording" }
func (v *recordingValidator) Close() error { return nil }

func (v *recordingValidator) Validate(_ context.Context, query string, args []any) error {
	v.query, v.args = query, args
	return nil
}

// Validate must see the query exactly as a run sends it: bound text as $N plus
// args for postgres, inlined as a JSON string for elasticsearch.
func TestValidateOne_PassesBoundQueryTextTheWayTheEngineTypeReceivesIt(t *testing.T) {
	loaded, err := suite.Parse([]byte(`schema_version: 1
id: validate_bound
queries:
  - id: q-trust
    engines:
      pg:
        query: "SELECT id FROM articles WHERE search_vector @@ plainto_tsquery('english', {{$terms}})"
        params: { terms: "voters don't trust" }
      es:
        query: '{"query": {"match": {"title": {{$terms}}}}}'
        params: { terms: "voters don't trust" }
`))
	require.NoError(t, err)

	tests := []struct {
		engineType string
		engineName string
		assertSeen func(t *testing.T, v *recordingValidator)
	}{
		{
			engineType: "postgres",
			engineName: "pg",
			assertSeen: func(t *testing.T, v *recordingValidator) {
				assert.Equal(t, "SELECT id FROM articles WHERE search_vector @@ plainto_tsquery('english', $1)", v.query)
				assert.Equal(t, []any{"voters don't trust"}, v.args)
			},
		},
		{
			engineType: "elasticsearch",
			engineName: "es",
			assertSeen: func(t *testing.T, v *recordingValidator) {
				assert.JSONEq(t, `{"query": {"match": {"title": "voters don't trust"}}}`, v.query)
				assert.Empty(t, v.args)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.engineType, func(t *testing.T) {
			validator := &recordingValidator{}

			row := validateOne(context.Background(), validateInput{
				query:      loaded.Suite.Queries[0],
				engineName: tt.engineName,
				engineType: tt.engineType,
				binding:    spec.QueryBinding{QuerySource: tt.engineName},
				loaded:     loaded,
				executor:   validator,
			})

			require.Equal(t, "OK", row.status, row.detail)
			tt.assertSeen(t, validator)
		})
	}
}
