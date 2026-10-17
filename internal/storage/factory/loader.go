package factory

import (
	"context"
	"errors"
	"fmt"

	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/storage/es"
)

// LoaderConfig selects the engine a corpus is loaded into and how to reach it.
type LoaderConfig struct {
	Engine storage.Type
	Es     *es.ClientConfig
}

// NewCorpusLoader returns the engine's loader and a func that releases its
// connections.
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
