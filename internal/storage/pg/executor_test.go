package pg

import (
	"context"
	"flag"
	"os"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/storage"
	pkgtesting "github.com/DjordjeVuckovic/tusker/pkg/testing"
	"github.com/testcontainers/testcontainers-go"
)

var (
	testCtx      context.Context
	testPool     *ConnectionPool
	testExecutor *RawExecutor
)

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		// These are integration tests backed by a Postgres container.
		// Skip them in -short mode (used by CI) so a Docker daemon isn't required.
		os.Exit(0)
	}

	testCtx = context.Background()

	pg, err := pkgtesting.NewPGContainer(testCtx, pkgtesting.PGConfig{
		Database: "news_test_db",
		Username: "test",
		Password: "test",
	})
	if err != nil {
		panic(err)
	}
	defer testcontainers.TerminateContainer(pg.Container)

	testPool, err = NewConnectionPool(testCtx, PoolConfig{ConnStr: pg.ConnString})
	if err != nil {
		panic(err)
	}
	defer testPool.Close()

	testExecutor = NewRawExecutor(testPool)

	os.Exit(m.Run())
}

func truncateTable(t *testing.T) {
	t.Helper()
	_, err := testPool.GetConn().Exec(testCtx, "TRUNCATE TABLE articles CASCADE")
	if err != nil {
		t.Fatalf("failed to truncate table: %v", err)
	}
}

func TestNewRawExecutor(t *testing.T) {
	if testExecutor == nil {
		t.Fatal("expected non-nil executor")
	}
	if testExecutor.db == nil {
		t.Fatal("expected non-nil db field")
	}
}

func TestRawExecutor_Exec_SimpleQuery(t *testing.T) {
	truncateTable(t)
	defer truncateTable(t)

	_, err := testPool.GetConn().Exec(testCtx, `
		INSERT INTO articles (title, content, url, language)
		VALUES ($1, $2, $3, $4)
	`, "Test Article", "This is test content", "http://test.com", "english")
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	result, err := testExecutor.Exec(testCtx, "SELECT * FROM articles WHERE title @@ plainto_tsquery('english', 'test')", nil, nil)
	if err != nil {
		t.Fatalf("failed to execute query: %v", err)
	}

	if result.TotalHits != 1 {
		t.Errorf("expected 1 hit, got %d", result.TotalHits)
	}
	if len(result.Hits) != 1 {
		t.Fatalf("expected 1 hit in results, got %d", len(result.Hits))
	}

	hit := result.Hits[0]
	if title, ok := hit["title"].(string); !ok || title != "Test Article" {
		t.Errorf("expected title 'Test Article', got %v", hit["title"])
	}
}

func TestRawExecutor_Exec_ParameterizedQuery(t *testing.T) {
	truncateTable(t)
	defer truncateTable(t)

	_, err := testPool.GetConn().Exec(testCtx, `
		INSERT INTO articles (title, content, url, language)
		VALUES ($1, $2, $3, $4), ($5, $6, $7, $8)
	`,
		"Climate Change", "Article about climate", "http://climate.com", "english",
		"Technology News", "Article about tech", "http://tech.com", "english",
	)
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	result, err := testExecutor.Exec(testCtx, `
		SELECT * FROM articles WHERE title @@ to_tsquery('english', $1::text)
	`, []interface{}{"climate | change"}, nil)
	if err != nil {
		t.Fatalf("failed to execute query: %v", err)
	}

	if result.TotalHits != 1 {
		t.Errorf("expected 1 hit, got %d", result.TotalHits)
	}

	hit := result.Hits[0]
	if title, ok := hit["title"].(string); !ok || title != "Climate Change" {
		t.Errorf("expected title 'Climate Change', got %v", hit["title"])
	}
}

func TestRawExecutor_Exec_MultipleParameters(t *testing.T) {
	truncateTable(t)
	defer truncateTable(t)

	_, err := testPool.GetConn().Exec(testCtx, `
		INSERT INTO articles (title, content, url, language, author)
		VALUES ($1, $2, $3, $4, $5)
	`, "Test Article", "Content here", "http://test.com", "english", "John Doe")
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	result, err := testExecutor.Exec(testCtx, `
		SELECT * FROM articles WHERE title = $1 AND author = $2
	`, []interface{}{"Test Article", "John Doe"}, nil)
	if err != nil {
		t.Fatalf("failed to execute query: %v", err)
	}

	if result.TotalHits != 1 {
		t.Errorf("expected 1 hit, got %d", result.TotalHits)
	}
}

func TestRawExecutor_Exec_EmptyResults(t *testing.T) {
	truncateTable(t)
	defer truncateTable(t)

	result, err := testExecutor.Exec(testCtx, `
		SELECT * FROM articles WHERE title = $1
	`, []interface{}{"NonExistent"}, nil)
	if err != nil {
		t.Fatalf("failed to execute query: %v", err)
	}

	if result.TotalHits != 0 {
		t.Errorf("expected 0 hits, got %d", result.TotalHits)
	}
	if len(result.Hits) != 0 {
		t.Errorf("expected 0 hits in results, got %d", len(result.Hits))
	}
}

func TestRawExecutor_Exec_WithTimeout(t *testing.T) {
	truncateTable(t)
	defer truncateTable(t)

	_, err := testPool.GetConn().Exec(testCtx, `
		INSERT INTO articles (title, content, url, language)
		VALUES ($1, $2, $3, $4)
	`, "Test Article", "Content", "http://test.com", "english")
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	opts := &storage.ExecOptions{TimeoutSeconds: 5}
	result, err := testExecutor.Exec(testCtx, `
		SELECT * FROM articles
	`, nil, opts)
	if err != nil {
		t.Fatalf("failed to execute query: %v", err)
	}

	if result.TotalHits != 1 {
		t.Errorf("expected 1 hit, got %d", result.TotalHits)
	}
}

func TestRawExecutor_Exec_MultipleRows(t *testing.T) {
	truncateTable(t)
	defer truncateTable(t)

	for i := 0; i < 5; i++ {
		_, err := testPool.GetConn().Exec(testCtx, `
			INSERT INTO articles (title, content, url, language)
			VALUES ($1, $2, $3, $4)
		`, "Article Title", "Content", "http://test.com", "english")
		if err != nil {
			t.Fatalf("failed to insert test data: %v", err)
		}
	}

	result, err := testExecutor.Exec(testCtx, `
		SELECT * FROM articles ORDER BY created_at
	`, nil, nil)
	if err != nil {
		t.Fatalf("failed to execute query: %v", err)
	}

	if result.TotalHits != 5 {
		t.Errorf("expected 5 hits, got %d", result.TotalHits)
	}
	if len(result.Hits) != 5 {
		t.Fatalf("expected 5 hits in results, got %d", len(result.Hits))
	}
}

func TestRawExecutor_Exec_AllFieldsReturned(t *testing.T) {
	truncateTable(t)
	defer truncateTable(t)

	_, err := testPool.GetConn().Exec(testCtx, `
		INSERT INTO articles (title, subtitle, content, author, url, metadata, language, description)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		"Full Test Article",
		"Test Subtitle",
		"This is the full test content for the article",
		"Test Author",
		"http://full-test.com",
		`{"key": "value", "number": 42}`,
		"english",
		"Test description",
	)
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	result, err := testExecutor.Exec(testCtx, `
		SELECT title, subtitle, content, author, url, metadata, language, description
		FROM articles
	`, nil, nil)
	if err != nil {
		t.Fatalf("failed to execute query: %v", err)
	}

	if result.TotalHits != 1 {
		t.Fatalf("expected 1 hit, got %d", result.TotalHits)
	}

	hit := result.Hits[0]

	expectedFields := map[string]interface{}{
		"title":       "Full Test Article",
		"subtitle":    "Test Subtitle",
		"content":     "This is the full test content for the article",
		"author":      "Test Author",
		"url":         "http://full-test.com",
		"language":    "english",
		"description": "Test description",
	}

	for field, expectedValue := range expectedFields {
		actualValue, ok := hit[field]
		if !ok {
			t.Errorf("expected field %s to be present", field)
			continue
		}
		if actualValue != expectedValue {
			t.Errorf("field %s: expected %v, got %v", field, expectedValue, actualValue)
		}
	}

	if _, ok := hit["metadata"]; !ok {
		t.Error("expected metadata field to be present")
	}
}
