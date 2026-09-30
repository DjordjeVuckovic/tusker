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
