package document

import (
	"reflect"
	"time"

	"github.com/google/uuid"
)

const ArticleDefaultLanguage = "english"

type Article struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Subtitle    string    `json:"subtitle,omitempty"`
	Content     string    `json:"content"`
	Author      string    `json:"author,omitempty"`
	Description string    `json:"description,omitempty"`
	Language    string    `json:"language,omitempty"`
	URL         string    `json:"url,omitempty" format:"uri"`

	PublishedAt time.Time `json:"publishedAt,omitzero"`
	CreatedAt   time.Time `json:"createdAt"`

	Metadata     ArticleMetadata `json:"metadata,omitzero"`
	SearchVector any             `json:"search_vector"`
}

type ArticleMetadata struct {
	SourceId   string    `json:"sourceId,omitempty"`
	SourceName string    `json:"sourceName,omitempty"`
	Category   string    `json:"category,omitempty"`
	ImportedAt time.Time `json:"importedAt,omitzero"`

	Extra map[string]any `json:"extra,omitempty"`
}

func (ar *Article) ContainsField(field string) bool {
	reflectType := reflect.TypeOf(*ar)
	_, found := reflectType.FieldByName(field)
	return found
}

func NewArticleID() uuid.UUID {
	return uuid.New()
}
