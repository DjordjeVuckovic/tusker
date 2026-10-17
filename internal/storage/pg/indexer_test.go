package pg

import (
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/DjordjeVuckovic/tusker/internal/types/language"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndexer_RefusesAnArticleWithoutACorpusLanguage(t *testing.T) {
	pool := newEmbedTestPool(t)
	indexer, err := NewIndexer(pool)
	require.NoError(t, err)

	for _, lang := range []language.Code{"", "english", "en"} {
		article := document.Article{ID: uuid.New(), URL: "https://example.com/" + string(lang), Title: "t", Language: lang}

		_, err := indexer.Save(testCtx, article)
		assert.Error(t, err, "Save with language %q", lang)
		assert.Error(t, indexer.SaveBulk(testCtx, []document.Article{article}), "SaveBulk with language %q", lang)

		got, err := NewArticleReader(pool).GetByIDs(testCtx, []uuid.UUID{article.ID})
		require.NoError(t, err)
		assert.Empty(t, got, "article with language %q was stored", lang)
	}
}
