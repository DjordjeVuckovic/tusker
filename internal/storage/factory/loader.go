package factory

import (
	"context"
	"errors"
	"fmt"

	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/storage/es"
)

type LoaderConfig struct {
	Engine storage.Type
	Es     *es.ClientConfig
}

// NewCorpusLoader returns the loader and a func that closes its client.
func NewCorpusLoader(_ context.Context, cfg LoaderConfig) (storage.CorpusLoader, func(), error) {
	switch cfg.Engine {
	case storage.ES:
		if cfg.Es == nil {
			return nil, nil, errors.New("elasticsearch config is not set")
		}
		client, err := es.NewClient(*cfg.Es)
		if err != nil {
			return nil, nil, fmt.Errorf("connect to %s: %w", cfg.Engine, err)
		}
		return es.NewCorpusLoader(client), client.Close, nil
	default:
		return nil, nil, fmt.Errorf("no corpus loader for engine %q", cfg.Engine)
	}
}
