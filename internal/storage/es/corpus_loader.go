package es

import (
	"context"
	"fmt"

	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/types/language"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
)

// refreshIntervalAfterLoad is declared rather than reset to null, so the
// interval a search sees is the one this build states, not a vendor default.
const refreshIntervalAfterLoad = "1s"

type CorpusLoader struct {
	client *Client
}

func NewCorpusLoader(client *Client) *CorpusLoader {
	return &CorpusLoader{client: client}
}

func (l *CorpusLoader) LoadCorpus(ctx context.Context, c storage.Corpus, opts storage.LoadOptions) (storage.LoadRecord, error) {
	index := l.client.indexName
	if c.Language != language.English {
		return storage.LoadRecord{}, fmt.Errorf("load corpus into %s: the index analyzes %q only, the corpus is %q", index, language.English, c.Language)
	}
	builder := NewIndexBuilder()
	settings := builder.buildSettings()
	settings.RefreshInterval = "-1"
	mappings := builder.buildMapping(c.Language.Analysis().ElasticsearchAnalyzer)
	if _, err := l.client.typed.Indices.Create(index).Settings(&settings).Mappings(&mappings).Do(ctx); err != nil {
		return storage.LoadRecord{}, fmt.Errorf("create index %s: %w", index, err)
	}

	restored := types.IndexSettings{RefreshInterval: refreshIntervalAfterLoad}
	if _, err := l.client.typed.Indices.PutSettings().Indices(index).Settings(&restored).Do(ctx); err != nil {
		return storage.LoadRecord{}, fmt.Errorf("restore refresh interval of %s: %w", index, err)
	}
	return storage.LoadRecord{}, nil
}
