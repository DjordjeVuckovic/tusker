package es

import (
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/densevectorsimilarity"
	"github.com/google/uuid"
)

// ArticleDocument ESDocument represents the document structure for Elasticsearch
type ArticleDocument struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Subtitle    string    `json:"subtitle"`
	Description string    `json:"description"`
	Content     string    `json:"content"`
	Author      string    `json:"author"`
	URL         string    `json:"url"`
	Language    string    `json:"language"`
	CreatedAt   time.Time `json:"created_at"`
	SourceId    string    `json:"source_id"`
	SourceName  string    `json:"source_name"`
	PublishedAt time.Time `json:"published_at"`
	Category    string    `json:"category"`
	ImportedAt  time.Time `json:"imported_at"`
	IndexedAt   time.Time `json:"indexed_at"`
}

type IndexBuilder struct {
	defaultLanguage string
}

func NewIndexBuilder() *IndexBuilder {
	return &IndexBuilder{
		defaultLanguage: document.ArticleDefaultLanguage,
	}
}

func (b *IndexBuilder) mapToESDocument(article document.Article) ArticleDocument {
	if article.ID == uuid.Nil {
		article.ID = uuid.New()
	}
	return ArticleDocument{
		ID:          article.ID.String(),
		Title:       article.Title,
		Subtitle:    article.Subtitle,
		Description: article.Description,
		Content:     article.Content,
		Author:      article.Author,
		URL:         article.URL,
		Language:    article.Language,
		CreatedAt:   article.CreatedAt,
		SourceId:    article.Metadata.SourceId,
		SourceName:  article.Metadata.SourceName,
		PublishedAt: article.Metadata.PublishedAt,
		Category:    article.Metadata.Category,
		ImportedAt:  article.Metadata.ImportedAt,
		IndexedAt:   time.Now(),
	}
}

func (b *IndexBuilder) buildSettings() types.IndexSettings {
	return types.IndexSettings{
		Analysis: &types.IndexSettingsAnalysis{
			Analyzer: map[string]types.Analyzer{
				"multilingual_analyzer": types.StandardAnalyzer{
					Stopwords: []string{"_none_"},
				},
			},
		},
	}
}

func (b *IndexBuilder) buildMapping() types.TypeMapping {
	return types.TypeMapping{
		Properties: map[string]types.Property{
			"id":           types.NewKeywordProperty(),
			"title":        b.createTextPropertyWithKeyword("multilingual_analyzer"),
			"subtitle":     b.createTextProperty("multilingual_analyzer"),
			"description":  b.createTextProperty("multilingual_analyzer"),
			"content":      b.createTextProperty("multilingual_analyzer"),
			"author":       b.createTextPropertyWithKeyword(""),
			"url":          types.NewKeywordProperty(),
			"language":     types.NewKeywordProperty(),
			"created_at":   types.NewDateProperty(),
			"source_id":    types.NewKeywordProperty(),
			"source_name":  b.createTextPropertyWithKeyword(""),
			"published_at": types.NewDateProperty(),
			"category":     types.NewKeywordProperty(),
			"imported_at":  types.NewDateProperty(),
			"indexed_at":   types.NewDateProperty(),
			// Document embedding lives on the article doc (see embedder.go).
			"embedding":       b.denseVectorProperty(),
			"embedding_model": types.NewKeywordProperty(),
		},
	}
}

// denseVectorProperty defines the kNN-searchable embedding field. The Qwen
// vectors are L2-normalised, so cosine similarity is the right metric.
func (b *IndexBuilder) denseVectorProperty() *types.DenseVectorProperty {
	p := types.NewDenseVectorProperty()
	dims := EmbeddingDims
	indexed := true
	sim := densevectorsimilarity.Cosine
	p.Dims = &dims
	p.Index = &indexed
	p.Similarity = &sim
	return p
}

func (b *IndexBuilder) createTextProperty(analyzer string) types.Property {
	textProp := types.NewTextProperty()
	if analyzer != "" {
		textProp.Analyzer = &analyzer
	}
	return textProp
}

func (b *IndexBuilder) createTextPropertyWithKeyword(analyzer string) types.Property {
	textProp := types.NewTextProperty()
	if analyzer != "" {
		textProp.Analyzer = &analyzer
	}
	textProp.Fields = map[string]types.Property{
		"keyword": types.NewKeywordProperty(),
	}
	return textProp
}
