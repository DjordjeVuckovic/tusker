package reader

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mapperFor(t *testing.T, idStrategy string) *ArticleMapper {
	t.Helper()

	cfg, err := NewYAMLConfigLoader(strings.NewReader(`
kind: DataMapper
version: v1
metadata:
  name: "Test"
dataset: test
` + idStrategy + `
fieldMappings:
  - source: "title"
    sourceType: "string"
    target: "Title"
    required: true
  - source: "link"
    sourceType: "url"
    target: "URL"
    required: false
`)).Load(false)
	require.NoError(t, err)

	mapper, err := NewArticleMapper(cfg)
	require.NoError(t, err)
	return mapper
}

const uuidV5OverLink = `
idStrategy:
  kind: uuidv5
  source: link
`

func TestUuidV5StrategyIsStableAcrossMappers(t *testing.T) {
	record := map[string]string{"title": "one", "link": "https://example.com/a"}

	first, err := mapperFor(t, uuidV5OverLink).Map(record)
	require.NoError(t, err)
	second, err := mapperFor(t, uuidV5OverLink).Map(record)
	require.NoError(t, err)

	assert.Equal(t, first.ID, second.ID)
}

func TestUuidV5StrategyDistinguishesSources(t *testing.T) {
	mapper := mapperFor(t, uuidV5OverLink)

	a, err := mapper.Map(map[string]string{"title": "one", "link": "https://example.com/a"})
	require.NoError(t, err)
	b, err := mapper.Map(map[string]string{"title": "one", "link": "https://example.com/b"})
	require.NoError(t, err)

	assert.NotEqual(t, a.ID, b.ID)
}

func TestUuidV5StrategyRejectsBlankSource(t *testing.T) {
	for _, blank := range []string{"", "   "} {
		t.Run("blank source "+blank, func(t *testing.T) {
			_, err := mapperFor(t, uuidV5OverLink).Map(map[string]string{"title": "one", "link": blank})

			var drop *MappingDropError
			require.True(t, errors.As(err, &drop), "expected a *MappingDropError, got %T", err)
			assert.Equal(t, "ID", drop.Target)
		})
	}
}

func TestRandomStrategyIsTheDefault(t *testing.T) {
	mapper := mapperFor(t, "")
	record := map[string]string{"title": "one", "link": "https://example.com/a"}

	first, err := mapper.Map(record)
	require.NoError(t, err)
	second, err := mapper.Map(record)
	require.NoError(t, err)

	assert.NotEqual(t, first.ID, second.ID)
	assert.NotEqual(t, uuid.Nil, first.ID)
}

func TestExplicitIdTargetOverridesStrategy(t *testing.T) {
	cfg, err := NewYAMLConfigLoader(strings.NewReader(`
kind: DataMapper
version: v1
metadata:
  name: "Test"
dataset: test
idStrategy:
  kind: uuidv5
  source: link
fieldMappings:
  - source: "id"
    sourceType: "uuid"
    target: "ID"
    required: false
  - source: "title"
    sourceType: "string"
    target: "Title"
    required: true
`)).Load(false)
	require.NoError(t, err)

	mapper, err := NewArticleMapper(cfg)
	require.NoError(t, err)

	sourceID := uuid.New()
	article, err := mapper.Map(map[string]string{
		"id": sourceID.String(), "title": "one", "link": "https://example.com/a",
	})
	require.NoError(t, err)

	assert.Equal(t, sourceID, article.ID)
}

func TestInvalidIdStrategyIsRejectedAtLoad(t *testing.T) {
	tests := []struct {
		name     string
		strategy string
		wantErr  string
	}{
		{name: "unknown kind", strategy: "idStrategy:\n  kind: sha256\n", wantErr: "unknown id strategy kind"},
		{name: "derived without source", strategy: "idStrategy:\n  kind: uuidv5\n", wantErr: "needs a source field"},
		{name: "random with source", strategy: "idStrategy:\n  kind: random\n  source: link\n", wantErr: "ignores source"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := NewYAMLConfigLoader(strings.NewReader(`
kind: DataMapper
version: v1
metadata:
  name: "Test"
dataset: test
` + tt.strategy + `
fieldMappings:
  - source: "title"
    sourceType: "string"
    target: "Title"
`)).Load(false)
			require.NoError(t, err)

			_, err = NewArticleMapper(cfg)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
