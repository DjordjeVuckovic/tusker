package es

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/api/dto"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/DjordjeVuckovic/tusker/internal/types/language"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/densevectorindexoptionstype"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/densevectorsimilarity"
	"github.com/google/uuid"
)

// ArticleDocument is an article as Elasticsearch stores it. The embedding
// fields are written separately, by the embedder.
type ArticleDocument struct {
	ID              string            `json:"id"`
	URL             string            `json:"url"`
	Title           string            `json:"title"`
	Description     string            `json:"description"`
	Content         string            `json:"content"`
	PublishedAt     *time.Time        `json:"published_at"`
	Language        string            `json:"language"`
	PublisherDomain string            `json:"publisher_domain"`
	Metadata        document.Metadata `json:"metadata"`
}

// optionalTime maps the zero time to an absent value on the wire, so the API
// never reports a year-1 publication date.
func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func (d ArticleDocument) toArticle() (document.Article, error) {
	id, err := uuid.Parse(d.ID)
	if err != nil {
		return document.Article{}, fmt.Errorf("parse article id %q: %w", d.ID, err)
	}
	article := document.Article{
		ID:              id,
		URL:             d.URL,
		Title:           d.Title,
		Description:     d.Description,
		Content:         d.Content,
		Language:        language.Code(d.Language),
		PublisherDomain: d.PublisherDomain,
		Metadata:        d.Metadata,
	}
	if d.PublishedAt != nil {
		article.PublishedAt = *d.PublishedAt
	}
	return article, nil
}

// decodeArticle reads a hit's _source into the API shape.
func decodeArticle(source json.RawMessage) (dto.Article, error) {
	var doc ArticleDocument
	if err := json.Unmarshal(source, &doc); err != nil {
		return dto.Article{}, fmt.Errorf("unmarshal article document: %w", err)
	}
	article, err := doc.toArticle()
	if err != nil {
		return dto.Article{}, err
	}
	return dto.NewArticle(article), nil
}

type IndexBuilder struct{}

func NewIndexBuilder() *IndexBuilder {
	return &IndexBuilder{}
}

func (b *IndexBuilder) mapToESDocument(article document.Article) (ArticleDocument, error) {
	if article.ID == uuid.Nil {
		article.ID = uuid.New()
	}
	if _, err := language.Parse(string(article.Language)); err != nil {
		return ArticleDocument{}, fmt.Errorf("article %s: %w", article.ID, err)
	}
	return ArticleDocument{
		ID:              article.ID.String(),
		URL:             article.URL,
		Title:           article.Title,
		Description:     article.Description,
		Content:         article.Content,
		PublishedAt:     optionalTime(article.PublishedAt),
		Language:        string(article.Language),
		PublisherDomain: article.PublisherDomain,
		Metadata:        article.Metadata,
	}, nil
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
			"id":               types.NewKeywordProperty(),
			"url":              types.NewKeywordProperty(),
			"title":            b.createTextPropertyWithKeyword("multilingual_analyzer"),
			"description":      b.createTextProperty("multilingual_analyzer"),
			"content":          b.createTextProperty("multilingual_analyzer"),
			"published_at":     types.NewDateProperty(),
			"language":         types.NewKeywordProperty(),
			"publisher_domain": types.NewKeywordProperty(),
			"metadata":         storedOnlyObject(),
			// Document embedding lives on the article doc (see embedder.go).
			"embedding":       b.denseVectorProperty(),
			"embedding_model": types.NewKeywordProperty(),
		},
	}
}

// storedOnlyObject keeps an object in _source without indexing or mapping any
// of its fields, so a new metadata key never grows the mapping.
func storedOnlyObject() *types.ObjectProperty {
	p := types.NewObjectProperty()
	enabled := false
	p.Enabled = &enabled
	return p
}

// denseVectorProperty defines the kNN-searchable embedding field. The Qwen
// vectors are L2-normalised, so cosine similarity is the right metric.
// index_options is declared because an unset one is neither shared with pgvector
// nor echoed back in _mapping.
func (b *IndexBuilder) denseVectorProperty() *types.DenseVectorProperty {
	p := types.NewDenseVectorProperty()
	dims := EmbeddingDims
	indexed := true
	sim := densevectorsimilarity.Cosine
	m := storage.HNSWM
	efConstruction := storage.HNSWEfConstruction
	p.Dims = &dims
	p.Index = &indexed
	p.Similarity = &sim
	p.IndexOptions = &types.DenseVectorIndexOptions{
		Type:           densevectorindexoptionstype.Hnsw,
		M:              &m,
		EfConstruction: &efConstruction,
	}
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
