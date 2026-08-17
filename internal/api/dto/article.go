package dto

import (
	"time"

	"github.com/google/uuid"
)

type Article struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Subtitle    string    `json:"subtitle,omitempty"`
	Content     string    `json:"content"`
	Author      string    `json:"author,omitempty"`
	Description string    `json:"description,omitempty"`
	Language    string    `json:"language,omitempty"`
	URL         string    `json:"url,omitempty" swaggertype:"string" format:"string"`

	PublishedAt *time.Time `json:"publishedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`

	Metadata ArticleMetadata `json:"metadata,omitzero"`
}

type ArticleMetadata struct {
	SourceId   string     `json:"sourceId,omitempty"`
	SourceName string     `json:"sourceName,omitempty"`
	Category   string     `json:"category,omitempty"`
	ImportedAt *time.Time `json:"importedAt,omitempty"`

	// Extra carries dataset-specific fields that have no schema field.
	Extra map[string]any `json:"extra,omitempty"`
}

type ArticleSearchResult struct {
	Article         `json:"article" ` // Embedded Article struct for search results
	Score           float64           `json:"score"`                      // Score rank between 0 and 1
	ScoreNormalized float64           `json:"score_normalized,omitempty"` // ScoreNormalized is the normalized(between 0-1) score
}
