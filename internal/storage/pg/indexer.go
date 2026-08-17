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

var insertColumns = []string{
	"id", "title", "subtitle", "content", "author", "description", "url",
	"language", "published_at", "created_at", "metadata",
}

func NewIndexer(pool *ConnectionPool) (*Indexer, error) {

	return &Indexer{db: pool.conn}, nil
}

func (s *Indexer) Save(ctx context.Context, article document.Article) (uuid.UUID, error) {
	row, err := insertRow(article, time.Now())
	if err != nil {
		return uuid.UUID{}, err
	}

	cmd := `
        INSERT INTO articles (id, title, subtitle, content, author, description, url, language,
                              published_at, created_at, metadata)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
        RETURNING id;
    `
	var id uuid.UUID
	if err := s.db.QueryRow(ctx, cmd, row...).Scan(&id); err != nil {
		return uuid.UUID{}, fmt.Errorf("failed to insert article: %w", err)
	}

	return id, nil
}

func (s *Indexer) SaveBulk(ctx context.Context, articles []document.Article) error {
	rows := make([][]any, len(articles))
	now := time.Now()

	for i, a := range articles {
		row, err := insertRow(a, now)
		if err != nil {
			return fmt.Errorf("article %d: %w", i, err)
		}
		rows[i] = row
	}

	_, err := s.db.CopyFrom(
		ctx,
		pgx.Identifier{"articles"},
		insertColumns,
		pgx.CopyFromRows(rows),
	)

	if err != nil {
		return fmt.Errorf("failed to bulk insert articles: %w", err)
	}
	return nil
}

func insertRow(a document.Article, now time.Time) ([]any, error) {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if a.Language == "" {
		a.Language = document.ArticleDefaultLanguage
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	if a.Metadata.ImportedAt.IsZero() {
		a.Metadata.ImportedAt = now
	}

	metadataJSON, err := json.Marshal(a.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return []any{
		a.ID,
		a.Title,
		a.Subtitle,
		a.Content,
		a.Author,
		a.Description,
		a.URL,
		a.Language,
		nullableTime(a.PublishedAt),
		a.CreatedAt,
		metadataJSON,
	}, nil
}
