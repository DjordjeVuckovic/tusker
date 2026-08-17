package pg

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/api/dto"
	"github.com/jackc/pgx/v5"
)

// ArticleColumns is the projection every article-returning query selects, in
// the order ScanArticle expects.
var ArticleColumns = []string{
	"id", "title", "subtitle", "content", "author", "description", "url",
	"language", "published_at", "created_at", "metadata",
}

// ArticleColumnList renders the projection, optionally table-qualified.
func ArticleColumnList(alias string) string {
	if alias == "" {
		return strings.Join(ArticleColumns, ", ")
	}
	qualified := make([]string, len(ArticleColumns))
	for i, c := range ArticleColumns {
		qualified[i] = alias + "." + c
	}
	return strings.Join(qualified, ", ")
}

// ScanArticle reads one row projected by ArticleColumns into a DTO. Trailing
// targets — a score, a window count — are scanned after the article columns.
func ScanArticle(rows pgx.Rows, trailing ...any) (*dto.Article, error) {
	var article dto.Article
	var metadataJSON []byte

	targets := append([]any{
		&article.ID,
		&article.Title,
		&article.Subtitle,
		&article.Content,
		&article.Author,
		&article.Description,
		&article.URL,
		&article.Language,
		&article.PublishedAt,
		&article.CreatedAt,
		&metadataJSON,
	}, trailing...)

	if err := rows.Scan(targets...); err != nil {
		return nil, fmt.Errorf("failed to scan article: %w", err)
	}

	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &article.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	return &article, nil
}

func MapToArticle(rows pgx.Rows) (*dto.Article, float32, error) {
	var distance float32
	article, err := ScanArticle(rows, &distance)
	if err != nil {
		return nil, 0, err
	}
	return article, distance, nil
}

// nullableTime maps the zero time to SQL NULL, so an article with no
// publication date sorts as unknown rather than as year 1.
func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
