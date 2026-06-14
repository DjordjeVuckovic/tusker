package reader

import (
	"time"

	"github.com/DjordjeVuckovic/news-hunter/internal/types/document"
)

// ToCanonicalRecord flattens an Article into the canonical flat record written
// by datapipe preprocess and read back by ArticleDirectMapper. The keys are the
// contract between the two — keep them in sync with ArticleDirectMapper.Map.
func ToCanonicalRecord(a document.Article) map[string]string {
	rec := map[string]string{
		"id":          a.ID.String(),
		"title":       a.Title,
		"subtitle":    a.Subtitle,
		"content":     a.Content,
		"author":      a.Author,
		"description": a.Description,
		"language":    a.Language,
		"url":         a.URL,
		"sourceId":    a.Metadata.SourceId,
		"sourceName":  a.Metadata.SourceName,
		"category":    a.Metadata.Category,
	}
	if !a.CreatedAt.IsZero() {
		rec["createdAt"] = a.CreatedAt.Format(time.RFC3339)
	}
	if !a.Metadata.PublishedAt.IsZero() {
		rec["publishedAt"] = a.Metadata.PublishedAt.Format(time.RFC3339)
	}
	if !a.Metadata.ImportedAt.IsZero() {
		rec["importedAt"] = a.Metadata.ImportedAt.Format(time.RFC3339)
	}
	return rec
}
