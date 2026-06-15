package pg

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/DjordjeVuckovic/tusker/internal/embedding"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

type Embedder struct {
	db *pgxpool.Pool
}

func NewEmbedder(pool *ConnectionPool) *Embedder {
	return &Embedder{db: pool.GetConn()}
}

func (e *Embedder) Save(ctx context.Context, article *embedding.Vec) (uuid.UUID, error) {
	vec := pgvector.NewVector(article.Embedding)
	cmd := `
		INSERT INTO article_embeddings (article_id, model_name, embedding)
		VALUES ($1, $2, $3)
		ON CONFLICT (article_id, model_name) DO UPDATE
		SET embedding = EXCLUDED.embedding
		RETURNING id
	`
	var id uuid.UUID
	err := e.db.QueryRow(
		ctx,
		cmd,
		article.ID,
		article.Model,
		vec,
	).Scan(&id)

	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to insert article embedding: %w", err)
	}

	return id, nil
}

// SaveBulk upserts a batch of embeddings. It COPYs into a temporary staging
// table, then inserts only rows whose article_id exists (orphans are skipped and
// logged) with ON CONFLICT upsert, making the operation re-runnable. DISTINCT ON
// collapses duplicate (article_id, model_name) rows within the batch, which would
// otherwise abort the upsert (Postgres forbids hitting a conflict row twice).
func (e *Embedder) SaveBulk(ctx context.Context, vecs []*embedding.Vec) error {
	if len(vecs) == 0 {
		return nil
	}

	tx, err := e.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		CREATE TEMP TABLE _embed_stage (
			article_id uuid,
			model_name text,
			embedding  vector
		) ON COMMIT DROP
	`)
	if err != nil {
		return fmt.Errorf("failed to create staging table: %w", err)
	}

	rows := make([][]any, len(vecs))
	for i, v := range vecs {
		rows[i] = []any{v.ID, v.Model, pgvector.NewVector(v.Embedding)}
	}

	_, err = tx.CopyFrom(
		ctx,
		pgx.Identifier{"_embed_stage"},
		[]string{"article_id", "model_name", "embedding"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("failed to copy embeddings to staging: %w", err)
	}

	tag, err := tx.Exec(ctx, `
		INSERT INTO article_embeddings (article_id, model_name, embedding)
		SELECT DISTINCT ON (s.article_id, s.model_name)
			s.article_id, s.model_name, s.embedding
		FROM _embed_stage s
		JOIN articles a ON a.id = s.article_id
		ORDER BY s.article_id, s.model_name
		ON CONFLICT (article_id, model_name) DO UPDATE
		SET embedding = EXCLUDED.embedding
	`)
	if err != nil {
		return fmt.Errorf("failed to upsert article embeddings: %w", err)
	}

	// Skipped = orphan article_id (no matching article) and/or in-batch duplicates.
	if skipped := int64(len(vecs)) - tag.RowsAffected(); skipped > 0 {
		slog.Warn("skipped embeddings (orphan article or duplicate id)",
			"skipped", skipped,
			"upserted", tag.RowsAffected(),
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit embeddings: %w", err)
	}

	return nil
}
