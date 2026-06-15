package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ArticleReader struct {
	db *pgxpool.Pool
}

func NewArticleReader(pool *ConnectionPool) *ArticleReader {
	return &ArticleReader{db: pool.GetConn()}
}

func (r *ArticleReader) GetByIDs(ctx context.Context, ids []uuid.UUID) ([]document.Article, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	pgUUIDs := make([]pgtype.UUID, len(ids))
	for i, id := range ids {
		pgUUIDs[i] = pgtype.UUID{Bytes: id, Valid: true}
	}

	const q = `
		SELECT id, title, subtitle, content, author, description, language, created_at
		FROM articles
		WHERE id = ANY($1)
	`

	rows, err := r.db.Query(ctx, q, pgUUIDs)
	if err != nil {
		return nil, fmt.Errorf("query articles by ids: %w", err)
	}
	defer rows.Close()

	var articles []document.Article
	for rows.Next() {
		var a document.Article
		var id pgtype.UUID
		var createdAt time.Time

		if err := rows.Scan(
			&id,
			&a.Title,
			&a.Subtitle,
			&a.Content,
			&a.Author,
			&a.Description,
			&a.Language,
			&createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan article row: %w", err)
		}

		a.ID = uuid.UUID(id.Bytes)
		a.CreatedAt = createdAt
		articles = append(articles, a)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate article rows: %w", err)
	}

	return articles, nil
}
