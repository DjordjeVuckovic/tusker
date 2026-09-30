package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/bench/suite"
	"github.com/DjordjeVuckovic/tusker/internal/storage/pg"
	pkgtesting "github.com/DjordjeVuckovic/tusker/pkg/testing"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const awkwardQueryText = `Ukraine's voters don't trust the "election"`

var yamlQuoted = strings.ReplaceAll(awkwardQueryText, "'", "''")

func resolveSuiteQuery(t *testing.T, suiteYAML string, dialect suite.Dialect) *suite.ResolvedQuery {
	t.Helper()
	loaded, err := suite.Parse([]byte(suiteYAML))
	require.NoError(t, err)
	resolved, err := loaded.Suite.Queries[0].ResolveEngineQuery(suite.ResolveOptions{
		Engine:   "engine",
		Registry: loaded.Registry,
		Dialect:  dialect,
	})
	require.NoError(t, err)
	return resolved
}

func TestPgExecutor_RunsSuiteQueryWithApostrophesAndQuotes(t *testing.T) {
	ctx := context.Background()
	container := pkgtesting.NewPGContainerWithCleanup(ctx, t)
	pool, err := pg.NewConnectionPool(ctx, pg.PoolConfig{ConnStr: container.ConnString})
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	for i, title := range []string{
		`Why Ukraine's voters don't trust the "election"`,
		"Voters in Ukraine trust the election results",
		"Election turnout falls as voters stay home",
	} {
		_, err := pool.GetConn().Exec(ctx,
			"INSERT INTO articles (title, content, url) VALUES ($1, $2, $3)",
			title, title, fmt.Sprintf("http://example.test/%d", i))
		require.NoError(t, err)
	}

	const ranked = "SELECT id FROM articles WHERE search_vector @@ plainto_tsquery('english', %s) " +
		"ORDER BY ts_rank(search_vector, plainto_tsquery('english', %s)) DESC, id LIMIT 10"
	exec := NewPgExecutor("pg", pool)
	resolved := resolveSuiteQuery(t, `schema_version: 1
id: bound
templates:
  - id: pg_fts
    query: "`+fmt.Sprintf(ranked, "{{$terms}}", "{{$terms}}")+`"
queries:
  - id: q-awkward
    engines:
      engine:
        template: pg_fts
        params: { terms: '`+yamlQuoted+`' }
`, exec.Dialect())

	got, err := exec.Execute(ctx, resolved.Query, resolved.Args)
	require.NoError(t, err)
	require.NoError(t, exec.Validate(ctx, resolved.Query, resolved.Args))

	want, err := NewPgExecutor("pg", pool).Execute(ctx, fmt.Sprintf(ranked, "$1", "$1"), []any{awkwardQueryText})
	require.NoError(t, err)
	require.NotEmpty(t, want.RankedDocIDs, "the corpus holds a matching article, so an empty result proves nothing")
	assert.Equal(t, want.RankedDocIDs, got.RankedDocIDs)
}

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

	exec := NewEsExecutor("es", container.Address, index)
	resolved := resolveSuiteQuery(t, `schema_version: 1
id: bound
queries:
  - id: q-awkward
    engines:
      engine:
        query: '{"query": {"match": {"title": {{$terms}}}}, "size": 10}'
        params: { terms: '`+yamlQuoted+`' }
`, exec.Dialect())

	got, err := exec.Execute(ctx, resolved.Query, resolved.Args)
	require.NoError(t, err)
	require.NoError(t, exec.Validate(ctx, resolved.Query, resolved.Args))

	handWritten, err := json.Marshal(map[string]any{
		"query": map[string]any{"match": map[string]any{"title": awkwardQueryText}},
		"size":  10,
	})
	require.NoError(t, err)
	want, err := exec.Execute(ctx, string(handWritten), nil)
	require.NoError(t, err)
	require.NotEmpty(t, want.RankedDocIDs, "the index holds a matching document, so an empty result proves nothing")
	assert.Equal(t, want.RankedDocIDs, got.RankedDocIDs)
}

func esRequest(t *testing.T, method, url string, body []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, url, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Less(t, resp.StatusCode, 300, "%s %s", method, url)
}
