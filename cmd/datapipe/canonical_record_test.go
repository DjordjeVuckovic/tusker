package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/ingest/reader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ccNewsCanonicalLine is the first record of the cc-news canonical corpus as
// preprocess wrote it. ccNewsRawRecord rebuilds its source row in the cc-news
// columns, so preprocessing it again must give back the same bytes.
const (
	ccNewsCanonicalLine = `{"id":"8ea66370-7731-5195-9cf0-cdb48e54f7b2","title":"Pdf download Establishing Air Medical Programs for the Next Generatio…","content":"We use your LinkedIn profile and activity data to personalize ads and to show you more relevant ads. You can change your ad preferences anytime.","description":"Read Pdf download Establishing Air Medical Programs for the Next Generation: Frameworks for both Developed and Developing Nations E-book full PDF Online Down…","url":"https://www.slideshare.net/tiwevoyo/pdf-download-establishing-air-medical-programs-for-the-next-generation-frameworks-for-both-developed-and-developing-nations-ebook-full","sourceName":"www.slideshare.net","publishedAt":"2018-03-19T00:00:00Z"}`
	ccNewsRawRecord     = `{"title": "Pdf download Establishing Air Medical Programs for the Next Generatio…", "text": "We use your LinkedIn profile and activity data to personalize ads and to show you more relevant ads. You can change your ad preferences anytime.", "description": "Read Pdf download Establishing Air Medical Programs for the Next Generation: Frameworks for both Developed and Developing Nations E-book full PDF Online Down…", "url": "https://www.slideshare.net/tiwevoyo/pdf-download-establishing-air-medical-programs-for-the-next-generation-frameworks-for-both-developed-and-developing-nations-ebook-full", "date": "2018-03-19 00:00:00", "domain": "www.slideshare.net"}`
)

func TestCCNewsCanonicalRecord(t *testing.T) {
	t.Run("preprocess reproduces it byte for byte", func(t *testing.T) {
		dir := t.TempDir()
		cfg := preprocessConfig{
			InputPath:   writeFixture(t, dir, "raw.jsonl", ccNewsRawRecord+"\n"),
			OutputPath:  filepath.Join(dir, "canonical.jsonl"),
			MappingPath: filepath.Join("..", "..", "datasets", "cc-news", "mapping.yaml"),
			Workers:     1,
		}
		require.NoError(t, runPreprocess(t.Context(), io.Discard, cfg))

		written, err := os.ReadFile(cfg.OutputPath)
		require.NoError(t, err)
		assert.Equal(t, ccNewsCanonicalLine+"\n", string(written))
	})

	t.Run("load reads its top-level sourceName onto the article", func(t *testing.T) {
		records, err := reader.NewJSONLReader(strings.NewReader(ccNewsCanonicalLine)).Read()
		require.NoError(t, err)
		require.Len(t, records, 1)

		article, err := reader.NewArticleDirectMapper().Map(records[0])
		require.NoError(t, err)
		assert.Equal(t, "www.slideshare.net", article.SourceName)
	})
}
