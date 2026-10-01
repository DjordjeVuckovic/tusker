package es_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/bench/engine"
	"github.com/DjordjeVuckovic/tusker/internal/bench/suite"
	pkgtesting "github.com/DjordjeVuckovic/tusker/pkg/testing"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const awkwardQueryText = `Ukraine's voters don't trust the "election"`

var yamlQuoted = strings.ReplaceAll(awkwardQueryText, "'", "''")

func TestEsExecutor_RunsSuiteQueryWithApostrophesAndQuotes(t *testing.T) {
	ctx := context.Background()
	container := pkgtesting.NewESContainer(ctx, t)
	const index = "news"

	for _, title := range []string{
		`Why Ukraine's voters don't trust the "election"`,
		"Voters in Ukraine trust the election results",
		"Stock markets rally on tech earnings",
	} {
		doc, err := json.Marshal(map[string]string{"id": uuid.NewString(), "title": title})
		require.NoError(t, err)
		esRequest(t, http.MethodPost, container.Address+"/"+index+"/_doc?refresh=true", doc)
	}

	exec := engine.NewEsExecutor("es", container.Address, index)
	loaded, err := suite.Parse([]byte(`schema_version: 1
id: bound
queries:
  - id: q-awkward
    engines:
      engine:
        query: '{"query": {"match": {"title": {{$terms}}}}, "size": 10}'
        params: { terms: '` + yamlQuoted + `' }
`))
	require.NoError(t, err)
	resolved, err := loaded.Suite.Queries[0].ResolveEngineQuery(suite.ResolveOptions{
		Engine:   "engine",
		Registry: loaded.Registry,
		Dialect:  suite.DialectJSON,
	})
	require.NoError(t, err)

	asRun := engine.Request{Query: resolved.Query, Args: resolved.Args}
	got, err := exec.Execute(ctx, asRun)
	require.NoError(t, err)
	require.NoError(t, exec.Validate(ctx, asRun))

	handWritten, err := json.Marshal(map[string]any{
		"query": map[string]any{"match": map[string]any{"title": awkwardQueryText}},
		"size":  10,
	})
	require.NoError(t, err)
	want, err := exec.Execute(ctx, engine.Request{Query: string(handWritten)})
	require.NoError(t, err)
	require.NotEmpty(t, want.RankedDocIDs, "the index holds a matching document, so an empty result proves nothing")
	assert.Equal(t, want.RankedDocIDs, got.RankedDocIDs)
}

func esRequest(t *testing.T, method, url string, body []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, url, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Less(t, resp.StatusCode, 300, "%s %s", method, url)
}
