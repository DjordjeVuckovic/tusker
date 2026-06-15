package pg

import (
	"encoding/json"
	"fmt"

	"github.com/DjordjeVuckovic/tusker/internal/api/dto"
	"github.com/jackc/pgx/v5"
)

func MapToArticle(rows pgx.Rows) (*dto.Article, float32, error) {
	var article dto.Article
	var metadataJSON []byte
	var distance float32

	if err := rows.Scan(
		&article.ID,
		&article.Title,
		&article.Subtitle,
		&article.Content,
		&article.Author,
		&article.Description,
		&article.URL,
		&article.Language,
		&article.CreatedAt,
		&metadataJSON,
		&distance,
	); err != nil {
		return nil, 0, fmt.Errorf("failed to scan article: %w", err)
	}

	if err := json.Unmarshal(metadataJSON, &article.Metadata); err != nil {
		return nil, 0, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &article, distance, nil
}
