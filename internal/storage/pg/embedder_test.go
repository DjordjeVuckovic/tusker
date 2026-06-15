package pg

import (
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/embedding"
	pkgtesting "github.com/DjordjeVuckovic/tusker/pkg/testing"
	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
)

func newEmbedTestPool(t *testing.T) *ConnectionPool {
	t.Helper()

	container := pkgtesting.NewPGContainerWithCleanup(testCtx, t)
	pool, err := NewConnectionPool(testCtx, PoolConfig{
		ConnStr:          container.ConnString,
		RegisterVecTypes: true,
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func insertArticle(t *testing.T, pool *ConnectionPool, title string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.GetConn().QueryRow(testCtx, `
		INSERT INTO articles (title, content, url, language)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, title, "content", "http://test.com/"+title, "english").Scan(&id)
	if err != nil {
		t.Fatalf("failed to insert article: %v", err)
	}
	return id
}

func vec(dim int, fill float32) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = fill
	}
	return v
}

func countEmbeddings(t *testing.T, pool *ConnectionPool) int {
	t.Helper()
	var n int
	if err := pool.GetConn().QueryRow(testCtx, `SELECT count(*) FROM article_embeddings`).Scan(&n); err != nil {
		t.Fatalf("failed to count embeddings: %v", err)
	}
	return n
}

func TestEmbedder_SaveBulk_UpsertAndSkipOrphans(t *testing.T) {
	pool := newEmbedTestPool(t)
	embedder := NewEmbedder(pool)

	a1 := insertArticle(t, pool, "first")
	a2 := insertArticle(t, pool, "second")
	orphan := uuid.New() // no matching article row

	const model = "qwen3-embedding:0.6b"
	batch := []*embedding.Vec{
		{ID: a1, Model: model, Embedding: vec(1024, 0.1)},
		{ID: a2, Model: model, Embedding: vec(1024, 0.2)},
		{ID: orphan, Model: model, Embedding: vec(1024, 0.3)},
	}

	if err := embedder.SaveBulk(testCtx, batch); err != nil {
		t.Fatalf("SaveBulk: %v", err)
	}

	if got := countEmbeddings(t, pool); got != 2 {
		t.Fatalf("expected 2 embeddings (orphan skipped), got %d", got)
	}

	// Re-run with a changed vector for a1 → upsert, no duplicate.
	batch[0].Embedding = vec(1024, 0.9)
	if err := embedder.SaveBulk(testCtx, batch); err != nil {
		t.Fatalf("SaveBulk re-run: %v", err)
	}
	if got := countEmbeddings(t, pool); got != 2 {
		t.Fatalf("expected 2 embeddings after re-run, got %d", got)
	}

	var dist float64
	err := pool.GetConn().QueryRow(testCtx, `
		SELECT embedding <-> $1 FROM article_embeddings WHERE article_id = $2
	`, pgvector.NewVector(vec(1024, 0.9)), a1).Scan(&dist)
	if err != nil {
		t.Fatalf("failed to read updated embedding: %v", err)
	}
	if dist > 1e-4 {
		t.Errorf("expected upserted embedding to match, got L2 distance %v", dist)
	}
}

func TestEmbedder_SaveBulk_DuplicateIDsInBatch(t *testing.T) {
	pool := newEmbedTestPool(t)
	embedder := NewEmbedder(pool)

	a1 := insertArticle(t, pool, "dup")
	const model = "qwen3-embedding:0.6b"

	// Same (article_id, model_name) twice in one batch must not abort the upsert.
	batch := []*embedding.Vec{
		{ID: a1, Model: model, Embedding: vec(1024, 0.1)},
		{ID: a1, Model: model, Embedding: vec(1024, 0.2)},
	}

	if err := embedder.SaveBulk(testCtx, batch); err != nil {
		t.Fatalf("SaveBulk with duplicate ids: %v", err)
	}

	if got := countEmbeddings(t, pool); got != 1 {
		t.Fatalf("expected 1 embedding after de-dup, got %d", got)
	}
}
