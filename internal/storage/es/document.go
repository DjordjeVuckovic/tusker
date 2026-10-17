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
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/snowballlanguage"
	"github.com/google/uuid"
)

// ArticleDocument omits the embedding fields, which the embedder writes.
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

// The searched text fields rank with BM25 at the parameters tiger's bm25 index
// declares, rather than at whatever defaults the Elasticsearch version ships.
const (
	textSimilarity = "article_bm25"
	bm25K1         = 1.2
	bm25B          = 0.75
)

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
	return newArticleDocument(article), nil
}

func newArticleDocument(article document.Article) ArticleDocument {
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
	}
}

// buildSettings declares every index knob the benchmark compares on rather than
// inheriting Elasticsearch's defaults: one shard, no replica on a single node,
// and the result window.
//
// The analyzer mirrors PostgreSQL's english regconfig: stopwords are removed
// before stemming, and the stemmer is Snowball, not the Porter stemmer behind
// the built-in english analyzer, which reduces "news" to "new". The standard
// tokenizer keeps "don't" whole where PostgreSQL's parser splits it at the
// apostrophe, so apostrophes become spaces before tokenizing.
func (b *IndexBuilder) buildSettings() types.IndexSettings {
	shards, replicas, maxResultWindow := "1", "0", 10001
	k1, lengthNormalization := types.Float64(bm25K1), types.Float64(bm25B)
	english := snowballlanguage.English
	return types.IndexSettings{
		NumberOfShards:   &shards,
		NumberOfReplicas: &replicas,
		MaxResultWindow:  &maxResultWindow,
		Similarity: map[string]types.SettingsSimilarity{
			textSimilarity: types.SettingsSimilarityBm25{Type: "BM25", K1: &k1, B: &lengthNormalization},
		},
		Analysis: &types.IndexSettingsAnalysis{
			Analyzer: map[string]types.Analyzer{
				language.English.Analysis().ElasticsearchAnalyzer: types.CustomAnalyzer{
					CharFilter: []string{"apostrophe_to_space"},
					Tokenizer:  "standard",
					Filter:     []string{"lowercase", "postgres_english_stop", "english_snowball"},
				},
			},
			CharFilter: map[string]types.CharFilter{
				"apostrophe_to_space": types.MappingCharFilter{
					Mappings: []string{"' => \\u0020", "’ => \\u0020"},
				},
			},
			Filter: map[string]types.TokenFilter{
				"postgres_english_stop": types.StopTokenFilter{Stopwords: PostgresEnglishStopwords},
				"english_snowball":      types.SnowballTokenFilter{Language: &english},
			},
		},
	}
}

func (b *IndexBuilder) buildMapping(analyzer string) types.TypeMapping {
	return types.TypeMapping{
		Properties: map[string]types.Property{
			"id":               types.NewKeywordProperty(),
			"url":              types.NewKeywordProperty(),
			"title":            b.createTextPropertyWithKeyword(analyzer),
			"description":      b.createTextProperty(analyzer),
			"content":          b.createTextProperty(analyzer),
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

// storedOnlyObject is unmapped, so a new metadata key never grows the mapping.
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
	return searchedTextProperty(analyzer)
}

func (b *IndexBuilder) createTextPropertyWithKeyword(analyzer string) types.Property {
	textProp := searchedTextProperty(analyzer)
	textProp.Fields = map[string]types.Property{
		"keyword": types.NewKeywordProperty(),
	}
	return textProp
}

func searchedTextProperty(analyzer string) *types.TextProperty {
	textProp := types.NewTextProperty()
	if analyzer != "" {
		textProp.Analyzer = &analyzer
	}
	similarity := textSimilarity
	textProp.Similarity = &similarity
	return textProp
}
