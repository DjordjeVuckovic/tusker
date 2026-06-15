package pg

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Indexer struct {
	db *pgxpool.Pool
}

func NewIndexer(pool *ConnectionPool) (*Indexer, error) {

	return &Indexer{db: pool.conn}, nil
}

func (s *Indexer) Save(ctx context.Context, article document.Article) (uuid.UUID, error) {
	if article.ID == uuid.Nil {
		article.ID = uuid.New()
	}
	if article.Language == "" {
		article.Language = document.ArticleDefaultLanguage
	}
	if article.CreatedAt.IsZero() {
		article.CreatedAt = time.Now()
	}

	if article.Metadata.ImportedAt.IsZero() {
		article.Metadata.ImportedAt = time.Now()
	}

	metadataJSON, err := json.Marshal(article.Metadata)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	cmd := `
        INSERT INTO articles (id, title, subtitle, content, author, description, url, language, created_at, metadata)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
        RETURNING id;
    `
	var id uuid.UUID
	err = s.db.QueryRow(
		ctx,
		cmd,
		article.ID,
		article.Title,
		article.Subtitle,
		article.Content,
		article.Author,
		article.Description,
		article.URL,
		article.Language,
		article.CreatedAt,
		metadataJSON,
	).Scan(&id)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("failed to insert article: %w", err)
	}

	return id, nil
}

func (s *Indexer) SaveBulk(ctx context.Context, articles []document.Article) error {
	rows := make([][]interface{}, len(articles))
	now := time.Now()

	for i, a := range articles {
		if a.ID == uuid.Nil {
			a.ID = uuid.New()
		}
		if a.Language == "" {
			a.Language = document.ArticleDefaultLanguage
		}
		if a.CreatedAt.IsZero() {
			a.CreatedAt = now
		}

		// Set ImportedAt if not already set
		if a.Metadata.ImportedAt.IsZero() {
			a.Metadata.ImportedAt = now
		}

		// Marshal metadata to JSON
		metadataJSON, err := json.Marshal(a.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata for article %d: %w", i, err)
		}

		rows[i] = []interface{}{
			a.ID,
			a.Title,
			a.Subtitle,
			a.Content,
			a.Author,
			a.Description,
			a.URL,
			a.Language,
			a.CreatedAt,
			metadataJSON,
		}
	}

	_, err := s.db.CopyFrom(
		ctx,
		pgx.Identifier{"articles"},
		[]string{"id", "title", "subtitle", "content", "author", "description", "url", "language", "created_at", "metadata"},
		pgx.CopyFromRows(rows),
	)

	if err != nil {
		return fmt.Errorf("failed to bulk insert articles: %w", err)
	}
	return nil
}
