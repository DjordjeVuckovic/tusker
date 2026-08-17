package reader

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestYAMLMapper_Map(t *testing.T) {
	mapper := createMapper(t)

	articleID := uuid.New()
	published := time.Now().UTC().Truncate(time.Second)
	urlStr := "https://example.com"

	record := map[string]string{
		"id":        articleID.String(),
		"title":     "Test ArticleReflectTest",
		"published": published.Format("2006-01-02T15:04:05Z"),
		"url":       urlStr,
		"domain":    "example.com",
		"image":     "https://example.com/a.png",
	}

	article, err := mapper.Map(record)
	require.NoError(t, err)

	assert.Equal(t, "Test ArticleReflectTest", article.Title)
	assert.Equal(t, published, article.PublishedAt)
	assert.Equal(t, urlStr, article.URL)
	assert.Equal(t, "example.com", article.Metadata.SourceName)
	assert.Equal(t, "https://example.com/a.png", article.Metadata.Extra["imageUrl"])
}

func TestYAMLMapper_Map_RequiredFieldDrops(t *testing.T) {
	base := map[string]string{
		"id":        uuid.New().String(),
		"title":     "Present",
		"published": "2024-10-01T12:00:00Z",
		"url":       "https://example.com",
		"domain":    "example.com",
	}

	tests := []struct {
		name   string
		mutate func(map[string]string)
		reason DropReason
		target string
	}{
		{
			name:   "empty required value",
			mutate: func(r map[string]string) { r["title"] = "" },
			reason: ReasonEmptyValue,
			target: "Title",
		},
		{
			name:   "whitespace-only required value",
			mutate: func(r map[string]string) { r["title"] = "   \t " },
			reason: ReasonEmptyValue,
			target: "Title",
		},
		{
			name:   "required source column absent",
			mutate: func(r map[string]string) { delete(r, "title") },
			reason: ReasonMissingColumn,
			target: "Title",
		},
		{
			name:   "required value fails to convert",
			mutate: func(r map[string]string) { r["published"] = "not a date" },
			reason: ReasonConversion,
			target: "PublishedAt",
		},
		{
			name:   "required url converts to empty",
			mutate: func(r map[string]string) { r["url"] = "!!! not a url" },
			reason: ReasonConversion,
			target: "URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapper := createMapper(t)

			record := make(map[string]string, len(base))
			for k, v := range base {
				record[k] = v
			}
			tt.mutate(record)

			_, err := mapper.Map(record)
			require.Error(t, err)

			var drop *MappingDropError
			require.True(t, errors.As(err, &drop), "expected a *MappingDropError, got %T", err)
			assert.Equal(t, tt.reason, drop.Reason)
			assert.Equal(t, tt.target, drop.Target)
		})
	}
}

func TestYAMLMapper_Map_OptionalFieldsAreSkipped(t *testing.T) {
	mapper := createMapper(t)

	record := map[string]string{
		"id":        uuid.New().String(),
		"title":     "Present",
		"published": "2024-10-01T12:00:00Z",
		"url":       "https://example.com",
		// domain and image omitted entirely
	}

	article, err := mapper.Map(record)
	require.NoError(t, err)
	assert.Empty(t, article.Metadata.SourceName)
	assert.Empty(t, article.Metadata.Extra)
}

func TestNewArticleMapper_RejectsMovedTargets(t *testing.T) {
	moved := map[string]string{
		"Metadata.PublishedAt": "PublishedAt",
	}

	for target, replacement := range moved {
		t.Run(target, func(t *testing.T) {
			cfg, err := NewYAMLConfigLoader(strings.NewReader(`
kind: DataMapping
version: v1
metadata:
  name: "Test"
dataset: test
fieldMappings:
  - source: "x"
    sourceType: "string"
    target: "` + target + `"
`)).Load(false)
			require.NoError(t, err)

			_, err = NewArticleMapper(cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), replacement)
		})
	}
}

func TestNewArticleMapper_RejectsUnknownTarget(t *testing.T) {
	cfg, err := NewYAMLConfigLoader(strings.NewReader(`
kind: DataMapping
version: v1
metadata:
  name: "Test"
dataset: test
fieldMappings:
  - source: "x"
    sourceType: "string"
    target: "Nonsense"
`)).Load(false)
	require.NoError(t, err)

	_, err = NewArticleMapper(cfg)
	require.ErrorContains(t, err, `unknown target "Nonsense"`)
}

func createMapper(t *testing.T) *ArticleMapper {
	t.Helper()

	yamlContent := `
kind: DataMapping
version: v1
metadata:
  name: "Test"
dataset: test
fieldMappings:
  - source: "title"
    sourceType: "string"
    target: "Title"
    targetType: "string"
    required: true
  - source: "published"
    sourceType: "datetime"
    target: "PublishedAt"
    targetType: "datetime"
    required: true
  - source: "url"
    sourceType: "url"
    target: "URL"
    targetType: "url"
    required: true
  - source: "domain"
    sourceType: "string"
    target: "Metadata.SourceName"
    targetType: "string"
    required: false
  - source: "image"
    sourceType: "string"
    target: "Metadata.Extra.imageUrl"
    targetType: "string"
    required: false
dateFormat: "2006-01-02T15:04:05Z"
`
	cfg, err := NewYAMLConfigLoader(strings.NewReader(yamlContent)).Load(false)
	require.NoError(t, err)

	mapper, err := NewArticleMapper(cfg)
	require.NoError(t, err)
	return mapper
}

func TestCSVReader_ReadParallel_MalformedRow(t *testing.T) {
	csvData := `id,title,author
1,Go Concurrency,John Doe
2,OnlyTitle
3,Interfaces in Go,Alice`

	ctx := t.Context()
	reader := NewCSVReader(strings.NewReader(csvData))

	resultsChan, err := reader.ReadParallel(ctx, 2)
	require.NoError(t, err)

	var validResults []map[string]string
	var errorCount int

	for res := range resultsChan {
		if res.Err != nil {
			errorCount++
			continue
		}
		validResults = append(validResults, res.Record)
	}

	// We expect one malformed line ("2,OnlyTitle") with missing columns
	assert.Equal(t, 2, len(validResults))     // 2 valid rows
	assert.Equal(t, 1, errorCount)            // 1 row caused an error
	assert.Contains(t, validResults[0], "id") // sanity check
}
