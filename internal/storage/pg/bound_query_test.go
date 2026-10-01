package pg_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/bench/engine"
	"github.com/DjordjeVuckovic/tusker/internal/bench/suite"
	"github.com/DjordjeVuckovic/tusker/internal/storage/pg"
	pkgtesting "github.com/DjordjeVuckovic/tusker/pkg/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const awkwardQueryText = `Ukraine's voters don't trust the "election"`

var yamlQuoted = strings.ReplaceAll(awkwardQueryText, "'", "''")

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
	exec := engine.NewPgExecutor("pg", pool)
	loaded, err := suite.Parse([]byte(`schema_version: 1
id: bound
templates:
  - id: pg_fts
    query: "` + fmt.Sprintf(ranked, "{{$terms}}", "{{$terms}}") + `"
queries:
  - id: q-awkward
    engines:
      engine:
        template: pg_fts
        params: { terms: '` + yamlQuoted + `' }
`))
	require.NoError(t, err)
	resolved, err := loaded.Suite.Queries[0].ResolveEngineQuery(suite.ResolveOptions{
		Engine:   "engine",
		Registry: loaded.Registry,
		Dialect:  suite.DialectPostgres,
	})
	require.NoError(t, err)

	asRun := engine.Request{Query: resolved.Query, Args: resolved.Args}
	got, err := exec.Execute(ctx, asRun)
	require.NoError(t, err)
	require.NoError(t, exec.Validate(ctx, asRun))

	want, err := engine.NewPgExecutor("pg", pool).Execute(ctx, engine.Request{Query: fmt.Sprintf(ranked, "$1", "$1"), Args: []any{awkwardQueryText}})
	require.NoError(t, err)
	require.NotEmpty(t, want.RankedDocIDs, "the corpus holds a matching article, so an empty result proves nothing")
	assert.Equal(t, want.RankedDocIDs, got.RankedDocIDs)
}
