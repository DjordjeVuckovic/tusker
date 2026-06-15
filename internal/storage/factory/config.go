package factory

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/storage/es"
	"github.com/DjordjeVuckovic/tusker/internal/storage/pg"
)

type StorageConfig struct {
	storage.Type
	Pg *pg.PoolConfig
	Es *es.ClientConfig
}

func LoadEnv() (*StorageConfig, error) {
	storageType := (storage.Type)(os.Getenv("STORAGE_TYPE"))
	if storageType == "" {
		slog.Error("STORAGE_TYPE environment variable is not set")
		return nil, fmt.Errorf("STORAGE_TYPE environment variable is not set")
	}
	if storageType != storage.ES && storageType != storage.PG && storageType != storage.InMem {
		slog.Error("Invalid STORAGE_TYPE environment variable value", "value", storageType)
		return nil, fmt.Errorf(
			"invalid STORAGE_TYPE environment variable value: %s, expected one of %v",
			storageType,
			[]storage.Type{storage.ES, storage.PG, storage.InMem})
	}

	var esCfg *es.ClientConfig
	if storageType == storage.ES {
		esCfg = &es.ClientConfig{
			Addresses: strings.Split(os.Getenv("ES_ADDRESSES"), ","),
			IndexName: os.Getenv("ES_INDEX_NAME"),
			Username:  os.Getenv("ES_USERNAME"),
			Password:  os.Getenv("ES_PASSWORD"),
		}
		if len(esCfg.Addresses) == 0 || esCfg.IndexName == "" {
			slog.Error("Elasticsearch configuration is incomplete", "addresses", esCfg.Addresses, "indexName", esCfg.IndexName)
			return nil, fmt.Errorf("elasticsearch configuration is incomplete: addresses or index name is missing")
		}
	}

	var pgCfg *pg.PoolConfig
	if storageType == storage.PG {
		pgCfg = &pg.PoolConfig{
			ConnStr: os.Getenv("PG_CONNECTION_STRING"),
		}
		if pgCfg.ConnStr == "" {
			slog.Error("PostgreSQL connection string is not set")
			return nil, fmt.Errorf("PostgreSQL connection string is not set")
		}
	}

	return &StorageConfig{
		Type: storageType,
		Pg:   pgCfg,
		Es:   esCfg,
	}, nil
}
