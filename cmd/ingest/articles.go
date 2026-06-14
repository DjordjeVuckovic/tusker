package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/DjordjeVuckovic/news-hunter/internal/embedding"
	"github.com/DjordjeVuckovic/news-hunter/internal/ingest"
	"github.com/DjordjeVuckovic/news-hunter/internal/ingest/reader"
	"github.com/DjordjeVuckovic/news-hunter/internal/storage/factory"
	"github.com/DjordjeVuckovic/news-hunter/internal/types/document"
	"github.com/DjordjeVuckovic/news-hunter/pkg/config/env"
	"github.com/spf13/cobra"
)

func newArticlesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "articles",
		Short: "Map and index a news dataset into the articles store",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadArticlesConfig()
			if err != nil {
				return err
			}
			return runArticles(cmd.Context(), cfg)
		},
	}
}

type ArticlesConfig struct {
	DatasetPath     string
	DataMappingPath string
	MappingEnabled  bool
	BulkOptions     *struct {
		Enabled bool
		Size    int
	}
	factory.StorageConfig
	Embedding embedding.Config
}

func loadArticlesConfig() (*ArticlesConfig, error) {
	if err := env.LoadDotEnv(os.Getenv("ENV"), "cmd/ingest/articles.env"); err != nil {
		slog.Info("Skipping .env environment variables...", "error", err)
	}

	storageCfg, err := factory.LoadEnv()
	if err != nil {
		return nil, fmt.Errorf("load storage config: %w", err)
	}

	// Mapping is enabled by default. Set MAPPING_ENABLED=false to ingest an
	// already-canonical dataset (produced by cmd/preprocessor) via the direct mapper.
	mappingEnabled := os.Getenv("MAPPING_ENABLED") != "false"

	mappingPath := os.Getenv("MAPPING_CONFIG_PATH")
	if mappingEnabled && mappingPath == "" {
		return nil, fmt.Errorf("MAPPING_CONFIG_PATH environment variable is not set")
	}

	dsPath := os.Getenv("DATASET_PATH")
	if dsPath == "" {
		return nil, fmt.Errorf("DATASET_PATH environment variable is not set")
	}

	bulkSizeNum, err := strconv.Atoi(os.Getenv("BULK_SIZE"))
	if err != nil {
		bulkSizeNum = 5_000
	}

	embed, err := embedding.LoadConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("load embedding config: %w", err)
	}

	return &ArticlesConfig{
		DatasetPath:     dsPath,
		DataMappingPath: mappingPath,
		MappingEnabled:  mappingEnabled,
		BulkOptions: &struct {
			Enabled bool
			Size    int
		}{
			Enabled: os.Getenv("BULK_ENABLED") == "true",
			Size:    bulkSizeNum,
		},
		StorageConfig: *storageCfg,
		Embedding:     *embed,
	}, nil
}

func runArticles(ctx context.Context, cfg *ArticlesConfig) error {
	dataFile, err := os.Open(cfg.DatasetPath)
	if err != nil {
		return fmt.Errorf("open dataset: %w", err)
	}
	defer dataFile.Close()

	var articleReader reader.RawParallelReader
	switch filepath.Ext(cfg.DatasetPath) {
	case ".jsonl":
		articleReader = reader.NewJSONLReader(dataFile)
	default:
		articleReader = reader.NewCSVReader(dataFile)
	}

	mapper, err := newMapper(cfg)
	if err != nil {
		return fmt.Errorf("create mapper: %w", err)
	}

	collector := ingest.NewArticleCollector(articleReader, mapper)

	pipeline, err := newPipeline(ctx, cfg, collector)
	if err != nil {
		return fmt.Errorf("create pipeline: %w", err)
	}

	if err := pipeline.Run(ctx); err != nil {
		return fmt.Errorf("run pipeline: %w", err)
	}
	return nil
}

// newMapper selects the record-to-Article mapper. When mapping is disabled the
// dataset is assumed to already be canonical (produced by cmd/preprocessor), so
// the direct mapper is used and no YAML config is required.
func newMapper(cfg *ArticlesConfig) (reader.Mapper, error) {
	if !cfg.MappingEnabled {
		slog.Info("Mapping disabled — using direct mapper (expects canonical dataset)")
		return reader.NewArticleDirectMapper(), nil
	}

	file, err := os.Open(cfg.DataMappingPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open mapping config: %w", err)
	}
	defer file.Close()

	mappingCfg, err := reader.NewYAMLConfigLoader(file).Load(true)
	if err != nil {
		return nil, fmt.Errorf("failed to load mapping config: %w", err)
	}
	return reader.NewArticleMapper(mappingCfg), nil
}

func newPipeline(
	ctx context.Context,
	cfg *ArticlesConfig,
	coll ingest.Collector[document.Article],
) (ingest.Pipeline, error) {
	slog.Info("Creating pipeline", "storageType", cfg.StorageConfig.Type)

	storer, err := factory.NewIndexer(ctx, cfg.StorageConfig)
	if err != nil {
		return nil, fmt.Errorf("create storer: %w", err)
	}

	var opts []ingest.PipelineOption
	if cfg.BulkOptions.Enabled {
		opts = append(opts, ingest.WithBulk(cfg.BulkOptions.Size))
	}

	if cfg.Embedding.Enabled {
		ollama, err := embedding.NewOllamaClient(cfg.Embedding.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("create embedder: %w", err)
		}
		embedder := embedding.NewEmbedder(ollama)
		storageEmbedder, err := factory.NewEmbedderIndexer(ctx, cfg.StorageConfig)
		if err != nil {
			return nil, fmt.Errorf("storer does not support embedding: %w", err)
		}
		opts = append(opts, ingest.WithEmbeddings(storageEmbedder, embedder))
	}

	return ingest.NewPipeline(coll, storer, opts...), nil
}
