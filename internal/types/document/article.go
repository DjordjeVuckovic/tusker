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

// articleIDNamespace seeds every derived article id. Changing it changes the id
// of every article in every corpus built with a derived strategy.
var articleIDNamespace = uuid.MustParse("697bf49f-ca14-4303-83eb-f9d05f7c7c42")

// DeriveArticleID returns the UUIDv5 of name, so the same name always yields the
// same id and preprocess can be re-run without renumbering a corpus.
func DeriveArticleID(name string) uuid.UUID {
	return uuid.NewSHA1(articleIDNamespace, []byte(name))
}
