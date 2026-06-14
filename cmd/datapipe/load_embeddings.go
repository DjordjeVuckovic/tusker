package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/DjordjeVuckovic/news-hunter/internal/embedding"
	"github.com/DjordjeVuckovic/news-hunter/internal/embedding/embedfile"
	"github.com/DjordjeVuckovic/news-hunter/internal/storage"
	"github.com/DjordjeVuckovic/news-hunter/internal/storage/factory"
	"github.com/DjordjeVuckovic/news-hunter/internal/storage/objectstore"
	"github.com/DjordjeVuckovic/news-hunter/pkg/config/env"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

const (
	expectedDim      = 1024
	defaultBatchSize = 5_000
)

func newLoadEmbeddingsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "embeddings",
		Short: "Load precomputed embeddings from a file or object store",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadEmbeddingsConfig()
			if err != nil {
				return err
			}
			return runEmbeddings(cmd.Context(), cfg)
		},
	}
}

type EmbeddingsConfig struct {
	factory.StorageConfig
	Embedding embedding.Config
	BatchSize int
}

func loadEmbeddingsConfig() (*EmbeddingsConfig, error) {
	if err := env.LoadDotEnv(os.Getenv("ENV"), "cmd/datapipe/embeddings.env"); err != nil {
		slog.Info("Skipping .env environment variables...", "error", err)
	}

	storageCfg, err := factory.LoadEnv()
	if err != nil {
		return nil, err
	}

	embedCfg, err := embedding.LoadConfigFromEnv()
	if err != nil {
		return nil, err
	}

	if embedCfg.Source != embedding.SourceFile {
		return nil, fmt.Errorf("load embeddings requires EMBEDDING_SOURCE=file, got %q", embedCfg.Source)
	}

	store := embedCfg.ObjectStore
	if store.LocalPath == "" && (store.Bucket == "" || store.Key == "") {
		return nil, fmt.Errorf("set EMBEDDING_FILE_PATH or EMBEDDING_S3_BUCKET + EMBEDDING_S3_KEY")
	}

	batchSize := defaultBatchSize
	if v := os.Getenv("EMBEDDING_BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			batchSize = n
		}
	}

	return &EmbeddingsConfig{
		StorageConfig: *storageCfg,
		Embedding:     *embedCfg,
		BatchSize:     batchSize,
	}, nil
}

func runEmbeddings(ctx context.Context, cfg *EmbeddingsConfig) error {
	start := time.Now()

	path, cleanup, err := resolveFile(ctx, cfg.Embedding.ObjectStore)
	if err != nil {
		return err
	}
	defer cleanup()

	reader, err := embedfile.Open(path)
	if err != nil {
		return err
	}
	defer reader.Close()

	meta := reader.Meta()

	// The file's declared dimension, when present, must match the column width.
	if meta.Dim != 0 && meta.Dim != expectedDim {
		return fmt.Errorf("embeddings file dim %d does not match expected %d", meta.Dim, expectedDim)
	}

	model := meta.Model
	if cfg.Embedding.Model != "" {
		if meta.Model != "" && cfg.Embedding.Model != meta.Model {
			slog.Warn("overriding embeddings file model metadata",
				"file_model", meta.Model,
				"override", cfg.Embedding.Model,
			)
		}
		model = cfg.Embedding.Model
	}
	if model == "" {
		return errors.New("embeddings file has no model metadata; set EMBEDDING_MODEL")
	}

	slog.Info("🛫 Loading precomputed embeddings",
		"file", path,
		"model", model,
		"dim", meta.Dim,
		"pooling", meta.Pooling,
		"normalized", meta.Normalized,
		"row_count", meta.RowCount,
		"created_at", meta.CreatedAt,
	)

	indexer, err := factory.NewEmbedderIndexer(ctx, cfg.StorageConfig)
	if err != nil {
		return err
	}

	processed, badIDs, badDim, err := ingestEmbeddings(ctx, reader, indexer, model, cfg.BatchSize)
	if err != nil {
		return err
	}

	if processed == 0 && badIDs+badDim > 0 {
		return fmt.Errorf("ingested 0 embeddings: %d parse failures, %d dim mismatches", badIDs, badDim)
	}

	slog.Info("✅ Embedding ingest complete",
		"processed", processed,
		"parse_failures", badIDs,
		"dim_mismatches", badDim,
		"duration", time.Since(start),
	)
	return nil
}

func ingestEmbeddings(
	ctx context.Context,
	reader *embedfile.Reader,
	indexer storage.EmbedIndexer,
	model string,
	batchSize int,
) (processed, badIDs, badDim int, err error) {
	buf := make([]embedfile.Record, batchSize)
	batch := make([]*embedding.Vec, 0, batchSize)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := indexer.SaveBulk(ctx, batch); err != nil {
			return err
		}
		processed += len(batch)
		slog.Info("Saved embedding batch", "count", len(batch), "total", processed)
		batch = batch[:0]
		return nil
	}

	for {
		if ctx.Err() != nil {
			return processed, badIDs, badDim, ctx.Err()
		}

		n, readErr := reader.Read(buf)
		for i := 0; i < n; i++ {
			rec := buf[i]
			id, parseErr := uuid.Parse(rec.ID)
			if parseErr != nil {
				badIDs++
				continue
			}
			if len(rec.Embedding) != expectedDim {
				badDim++
				continue
			}
			batch = append(batch, &embedding.Vec{
				ID:        id,
				Model:     model,
				Embedding: rec.Embedding,
			})
			if len(batch) >= batchSize {
				if err := flush(); err != nil {
					return processed, badIDs, badDim, err
				}
			}
		}

		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return processed, badIDs, badDim, readErr
		}
	}

	if err := flush(); err != nil {
		return processed, badIDs, badDim, err
	}
	return processed, badIDs, badDim, nil
}

// resolveFile returns a local path to the embeddings file, downloading from the
// object store when no local path is configured.
func resolveFile(ctx context.Context, cfg embedding.ObjectStoreConfig) (string, func(), error) {
	if cfg.LocalPath != "" {
		return cfg.LocalPath, func() {}, nil
	}

	client, err := objectstore.New(ctx, objectstore.Config{
		Endpoint:     cfg.Endpoint,
		Region:       cfg.Region,
		Bucket:       cfg.Bucket,
		AccessKey:    cfg.AccessKey,
		SecretKey:    cfg.SecretKey,
		UsePathStyle: cfg.UsePathStyle,
	})
	if err != nil {
		return "", func() {}, err
	}

	tmpFile, err := os.CreateTemp("", "embeddings-*.parquet")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temp file: %w", err)
	}
	tmp := tmpFile.Name()
	_ = tmpFile.Close()
	cleanup := func() { _ = os.Remove(tmp) }

	slog.Info("Downloading embeddings file", "bucket", cfg.Bucket, "key", cfg.Key, "dst", tmp)
	n, err := client.Download(ctx, cfg.Key, tmp)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	slog.Info("Downloaded embeddings file", "bytes", n)

	return tmp, cleanup, nil
}
