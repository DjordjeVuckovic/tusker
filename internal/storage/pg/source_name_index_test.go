package pg

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	pkgtesting "github.com/DjordjeVuckovic/tusker/pkg/testing"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSourceNameFilter_PlansAsAnIndexScan(t *testing.T) {
	const filter = `SELECT id FROM articles WHERE source_name = 'host7.com'`

	engines := []struct {
		name    string
		connect func(t *testing.T) *ConnectionPool
		query   string
		inPlan  string
	}{
		{
			name: "pg-native",
			connect: func(t *testing.T) *ConnectionPool {
				truncateTable(t)
				t.Cleanup(func() { truncateTable(t) })
				return testPool
			},
			query:  filter,
			inPlan: "idx_articles_source_name",
		},
		{
			name: "paradedb",
			connect: func(t *testing.T) *ConnectionPool {
				return poolFor(t, pkgtesting.NewParadeDBContainerWithCleanup(context.Background(), t))
			},
			query:  filter + ` AND id @@@ paradedb.match('title', 'title')`,
			inPlan: `{"term":{"field":"source_name","value":"host7.com"`,
		},
		{
			name: "tiger",
			connect: func(t *testing.T) *ConnectionPool {
				return poolFor(t, pkgtesting.NewTigerContainerWithCleanup(context.Background(), t))
			},
			query:  filter,
			inPlan: "idx_articles_source_name",
		},
	}

	for _, engine := range engines {
		t.Run(engine.name, func(t *testing.T) {
			ctx := context.Background()
			pool := engine.connect(t)
			db := pool.GetConn()

			if err := seedArticlesAcrossSources(ctx, pool); err != nil {
				t.Fatalf("seed articles: %v", err)
			}
			if _, err := db.Exec(ctx, `ANALYZE articles`); err != nil {
				t.Fatalf("analyze articles: %v", err)
			}

			var matched int
			if err := db.QueryRow(ctx, `SELECT count(*) FROM (`+engine.query+`) AS f`).Scan(&matched); err != nil {
				t.Fatalf("count filtered articles: %v", err)
			}
			if matched == 0 {
				t.Fatal("no article matches source_name, so the loader writes it elsewhere")
			}

			if plan := explain(t, db, engine.query); !strings.Contains(plan, engine.inPlan) {
				t.Errorf("source_name filter plan lacks %s; plan:\n%s", engine.inPlan, plan)
			}
		})
	}
}

// seedArticlesAcrossSources loads 20,000 articles from 200 hosts through the
// loader, so the index is checked against the column the loader writes.
func seedArticlesAcrossSources(ctx context.Context, pool *ConnectionPool) error {
	const articles, sources = 20000, 200
	indexer, err := NewIndexer(pool)
	if err != nil {
		return err
	}
	batch := make([]document.Article, articles)
	for i := range batch {
		batch[i] = document.Article{
			Title:      fmt.Sprintf("title %d", i),
			Content:    fmt.Sprintf("content %d", i),
			URL:        fmt.Sprintf("https://example.com/%d", i),
			SourceName: fmt.Sprintf("host%d.com", i%sources),
		}
	}
	return indexer.SaveBulk(ctx, batch)
}

func poolFor(t *testing.T, container *pkgtesting.PGContainer) *ConnectionPool {
	t.Helper()
	pool, err := NewConnectionPool(context.Background(), PoolConfig{ConnStr: container.ConnString})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func connectTo(t *testing.T, container *pkgtesting.PGContainer) *pgxpool.Pool {
	t.Helper()
	return poolFor(t, container).GetConn()
}

func explain(t *testing.T, db *pgxpool.Pool, query string) string {
	t.Helper()
	rows, err := db.Query(context.Background(), "EXPLAIN "+query)
	if err != nil {
		t.Fatalf("explain %s: %v", query, err)
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan line: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("explain %s: %v", query, err)
	}
	return strings.Join(lines, "\n")
}
