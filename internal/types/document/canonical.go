package document

import "time"

// CanonicalArticle is the flattened, serialization-friendly form of an Article
// used by the preprocess pipeline (JSONL and Parquet outputs).
type CanonicalArticle struct {
	ID          string `json:"id" parquet:"id"`
	Title       string `json:"title" parquet:"title"`
	Subtitle    string `json:"subtitle,omitempty" parquet:"subtitle,optional"`
	Content     string `json:"content" parquet:"content"`
	Author      string `json:"author,omitempty" parquet:"author,optional"`
	Description string `json:"description,omitempty" parquet:"description,optional"`
	Language    string `json:"language,omitempty" parquet:"language,optional"`
	URL         string `json:"url,omitempty" parquet:"url,optional"`
	SourceId    string `json:"sourceId,omitempty" parquet:"sourceId,optional"`
	SourceName  string `json:"sourceName,omitempty" parquet:"sourceName,optional"`
	Category    string `json:"category,omitempty" parquet:"category,optional"`
	CreatedAt   string `json:"createdAt,omitempty" parquet:"createdAt,optional"`
	PublishedAt string `json:"publishedAt,omitempty" parquet:"publishedAt,optional"`
	ImportedAt  string `json:"importedAt,omitempty" parquet:"importedAt,optional"`
}

// ToCanonical flattens an Article into a CanonicalArticle.
func (ar *Article) ToCanonical() CanonicalArticle {
	rec := CanonicalArticle{
		ID:          ar.ID.String(),
		Title:       ar.Title,
		Subtitle:    ar.Subtitle,
		Content:     ar.Content,
		Author:      ar.Author,
		Description: ar.Description,
		Language:    ar.Language,
		URL:         ar.URL,
		SourceId:    ar.Metadata.SourceId,
		SourceName:  ar.Metadata.SourceName,
		Category:    ar.Metadata.Category,
	}
	if !ar.CreatedAt.IsZero() {
		rec.CreatedAt = ar.CreatedAt.Format(time.RFC3339)
	}
	if !ar.Metadata.PublishedAt.IsZero() {
		rec.PublishedAt = ar.Metadata.PublishedAt.Format(time.RFC3339)
	}
	if !ar.Metadata.ImportedAt.IsZero() {
		rec.ImportedAt = ar.Metadata.ImportedAt.Format(time.RFC3339)
	}
	return rec
}
