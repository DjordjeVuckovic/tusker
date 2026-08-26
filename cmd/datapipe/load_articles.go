package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/cli"
	"github.com/mattn/go-isatty"

	"github.com/DjordjeVuckovic/tusker/internal/embedding"
	"github.com/DjordjeVuckovic/tusker/internal/ingest"
	"github.com/DjordjeVuckovic/tusker/internal/ingest/reader"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/storage/factory"
	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/DjordjeVuckovic/tusker/pkg/config/env"
	"github.com/spf13/cobra"
)

var articlesDatasetExtensions = map[FileExt]struct{}{
	ExtCSV:     {},
	ExtJSONL:   {},
	ExtParquet: {},
}

var articlesMappingExtensions = map[FileExt]struct{}{
	ExtYAML: {},
}

func newLoadArticlesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "articles",
		Short: "Map and index a news dataset into the articles store",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadArticlesConfig()
			if err != nil {
				return err
			}
			return runArticles(cmd.Context(), cmd.OutOrStdout(), cfg)
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
	// AllowEmbeddingReset skips the confirmation before a load drops stored vectors.
	AllowEmbeddingReset bool
}

func loadArticlesConfig() (*ArticlesConfig, error) {
	if err := env.LoadDotEnv(os.Getenv("ENV"), "cmd/datapipe/articles.env"); err != nil {
		slog.Info("Skipping .env environment variables...", "error", err)
	}

	storageCfg, err := factory.LoadEnv()
	if err != nil {
		return nil, fmt.Errorf("load storage config: %w", err)
	}

	// The core path is preprocess -> load -> embed, so the dataset is assumed to
	// be canonical. Set MAPPING_ENABLED=true to map a raw dataset inline instead,
	// which skips preprocess and mints new article IDs on every run.
	mappingEnabled := os.Getenv("MAPPING_ENABLED") == "true"

	mappingPath := os.Getenv("MAPPING_CONFIG_PATH")
	if mappingEnabled {
		if mappingPath == "" {
			return nil, fmt.Errorf("MAPPING_CONFIG_PATH environment variable is not set")
		}
		if err := validateFileExt(mappingPath, articlesMappingExtensions); err != nil {
			return nil, fmt.Errorf("invalid MAPPING_CONFIG_PATH: %w", err)
		}
	}

	dsPath := os.Getenv("DATASET_PATH")
	if dsPath == "" {
		return nil, fmt.Errorf("DATASET_PATH environment variable is not set")
	}
	if err := validateFileExt(dsPath, articlesDatasetExtensions); err != nil {
		return nil, fmt.Errorf("invalid DATASET_PATH: %w", err)
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
		StorageConfig:       *storageCfg,
		Embedding:           *embed,
		AllowEmbeddingReset: os.Getenv("ALLOW_EMBEDDING_RESET") == "true",
	}, nil
}

func runArticles(ctx context.Context, out io.Writer, cfg *ArticlesConfig) error {
	articleReader, dataset, err := OpenDatasetReader(cfg.DatasetPath)
	if err != nil {
		return fmt.Errorf("create dataset reader: %w", err)
	}
	defer dataset.Close()

	fields := datasetFields(cfg.DatasetPath, articleReader)
	fields = append(fields,
		cli.Field{Label: "target", Value: storageTarget(cfg.StorageConfig)},
		cli.Field{Label: "bulk", Value: bulkLabel(cfg.BulkOptions.Enabled, cfg.BulkOptions.Size)},
	)
	cli.Header(out, "tusker load articles", fields...)

	mapper, err := newMapper(cfg)
	if err != nil {
		return fmt.Errorf("create mapper: %w", err)
	}

	collector := ingest.NewArticleCollector(articleReader, mapper)

	storer, err := factory.NewIndexer(ctx, cfg.StorageConfig)
	if err != nil {
		return fmt.Errorf("create storer: %w", err)
	}

	confirm := resetConfirmer(promptOptions{
		AllowAlways: cfg.AllowEmbeddingReset,
		Interactive: isatty.IsTerminal(os.Stdin.Fd()),
		In:          os.Stdin,
		Out:         os.Stdout,
	})
	if err := guardStoredEmbeddings(ctx, storer, confirm); err != nil {
		return err
	}

	pipeline, err := newPipeline(ctx, cfg, storer, collector)
	if err != nil {
		return fmt.Errorf("create pipeline: %w", err)
	}

	outcome, err := pipeline.Run(ctx)
	if err != nil {
		return fmt.Errorf("run pipeline: %w", err)
	}

	cli.Summary(out, "Article load complete",
		cli.IntField("processed", outcome.Processed),
		cli.IntField("failed", outcome.Errors),
		cli.Field{Label: "duration", Value: outcome.Duration.Round(time.Millisecond).String()},
	)
	if outcome.Errors > 0 {
		cli.Warn(out, fmt.Sprintf("%d skipped — not in the corpus", outcome.Errors))
	}
	return nil
}

// newMapper selects the record-to-Article mapper. The default reads a canonical
// dataset and needs no YAML config; inline mapping is the opt-in path.
func newMapper(cfg *ArticlesConfig) (reader.Mapper, error) {
	if !cfg.MappingEnabled {
		slog.Info("Using direct mapper (expects canonical dataset)")
		return reader.NewArticleDirectMapper(), nil
	}

	slog.Warn("MAPPING_ENABLED=true — mapping a raw dataset inline; article IDs " +
		"are regenerated, so corpora loaded this way are not comparable across engines")

	file, err := os.Open(cfg.DataMappingPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open mapping config: %w", err)
	}
	defer file.Close()

	mappingCfg, err := reader.NewYAMLConfigLoader(file).Load(true)
	if err != nil {
		return nil, fmt.Errorf("failed to load mapping config: %w", err)
	}
	return reader.NewArticleMapper(mappingCfg)
}

func newPipeline(
	ctx context.Context,
	cfg *ArticlesConfig,
	storer storage.Indexer,
	coll ingest.Collector[document.Article],
) (ingest.Pipeline, error) {
	slog.Info("Creating pipeline", "storageType", cfg.StorageConfig.Type)

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

// confirmReset decides whether an article load may drop stored vectors.
type confirmReset func(embedded int64) (bool, error)

// guardStoredEmbeddings checks whether an article load would drop vectors that
// live on the documents it overwrites, and asks before it does.
func guardStoredEmbeddings(ctx context.Context, storer storage.Indexer, confirm confirmReset) error {
	counter, ok := storer.(storage.EmbeddedDocumentCounter)
	if !ok {
		return nil
	}

	embedded, err := counter.CountEmbedded(ctx)
	if err != nil {
		return fmt.Errorf("count stored embeddings: %w", err)
	}
	if embedded == 0 {
		return nil
	}

	confirmed, err := confirm(embedded)
	if err != nil {
		return err
	}
	if !confirmed {
		return errEmbeddingResetDeclined
	}

	slog.Warn("article load drops stored embeddings; re-run `datapipe load embeddings` to restore them",
		"embeddings", embedded)
	return nil
}

// errEmbeddingResetDeclined is a deliberate answer, not a failure, so callers
// can report it without wrapping it as a broken pipeline.
var errEmbeddingResetDeclined = errors.New("aborted: this load would drop stored embeddings")

// promptOptions configures how the reset confirmation is asked.
type promptOptions struct {
	AllowAlways bool
	Interactive bool
	In          io.Reader
	Out         io.Writer
}

// resetConfirmer picks how to ask. A scripted run proceeds rather than block on
// a stdin nobody is watching.
func resetConfirmer(opts promptOptions) confirmReset {
	if opts.AllowAlways || !opts.Interactive {
		return func(int64) (bool, error) { return true, nil }
	}
	reader := bufio.NewReader(opts.In)
	return func(embedded int64) (bool, error) {
		fmt.Fprintf(opts.Out,
			"%d documents already carry embeddings and this load will drop them.\n"+
				"Re-run `datapipe load embeddings` afterwards to restore them. Continue? [y/N]: ",
			embedded)
		answer, err := reader.ReadString('\n')
		// A line ending in EOF rather than a newline still carries the answer;
		// ctrl-D on an empty line reads as a decline, not a failure.
		if err != nil && !errors.Is(err, io.EOF) {
			return false, fmt.Errorf("read confirmation: %w", err)
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		return answer == "y" || answer == "yes", nil
	}
}

func bulkLabel(enabled bool, size int) string {
	if !enabled {
		return "off"
	}
	return fmt.Sprintf("%d per batch", size)
}
