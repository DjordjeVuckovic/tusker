package factory

import (
	"errors"
	"fmt"

	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/storage/es"
)

// CorpusLoaderConfig selects the backend a corpus is loaded into. The caller
// owns and closes the connection it passes in.
type CorpusLoaderConfig struct {
	Type     storage.Type
	EsClient *es.Client
}

func NewCorpusLoader(cfg CorpusLoaderConfig) (storage.CorpusLoader, error) {
	switch cfg.Type {
	case storage.ES:
		if cfg.EsClient == nil {
			return nil, errors.New("elasticsearch client is not set")
		}
		return es.NewCorpusLoader(cfg.EsClient), nil
	default:
		return nil, fmt.Errorf(string(storage.ErrUnsupportedStorer), cfg.Type)
	}
}
