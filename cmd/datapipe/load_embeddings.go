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

	"github.com/DjordjeVuckovic/tusker/internal/cli"

	"github.com/DjordjeVuckovic/tusker/internal/embedding"
	"github.com/DjordjeVuckovic/tusker/internal/embedding/embedfile"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/storage/factory"
	"github.com/DjordjeVuckovic/tusker/internal/storage/objectstore"
	"github.com/DjordjeVuckovic/tusker/pkg/config/env"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

const (
	expectedDim      = 1024
	defaultBatchSize = 5_000
)

var embeddingsFileExtensions = map[FileExt]struct{}{
	ExtParquet: {},
}

func newLoadEmbeddingsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "embeddings",
		Short: "Load precomputed embeddings from a file or object store",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadEmbeddingsConfig()
			if err != nil {
				return err
			}
			return runEmbeddings(cmd.Context(), cmd.OutOrStdout(), cfg)
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

	// The S3 key is validated here rather than the downloaded path, which
	// resolveFile always names *.parquet.
	embeddingsPath, envVariable := store.LocalPath, "EMBEDDING_FILE_PATH"
	if embeddingsPath == "" {
		embeddingsPath, envVariable = store.Key, "EMBEDDING_S3_KEY"
	}
	if err := validateFileExt(embeddingsPath, embeddingsFileExtensions); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", envVariable, err)
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

func runEmbeddings(ctx context.Context, out io.Writer, cfg *EmbeddingsConfig) error {
	start := time.Now()

	file, err := resolveFile(ctx, cfg.Embedding.ObjectStore)
	if err != nil {
		return err
	}
	defer file.Cleanup()

	reader, err := embedfile.Open(file.Path)
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

	// The header carries file, model, dim and rows; these are the recipe fields
	// it does not, and a mixed corpus is only diagnosable from them.
	slog.Info("embeddings file metadata",
		"pooling", meta.Pooling,
		"normalized", meta.Normalized,
		"created_at", meta.CreatedAt,
	)

	cli.Header(out, "tusker load embeddings",
		cli.Field{Label: "source", Value: file.Display},
		cli.ByteField("size", fileSize(file.Path)),
		cli.IntField("rows", meta.RowCount),
		cli.Field{Label: "model", Value: model},
		cli.IntField("dimensions", meta.Dim),
		cli.Field{Label: "target", Value: storageTarget(cfg.StorageConfig)},
		cli.IntField("batch", cfg.BatchSize),
	)

	indexer, err := factory.NewEmbedderIndexer(ctx, cfg.StorageConfig)
	if err != nil {
		return err
	}

	stats, err := embedIngest{
		Reader:    reader,
		Indexer:   indexer,
		Model:     model,
		BatchSize: cfg.BatchSize,
	}.run(ctx)
	if err != nil {
		return err
	}

	// Both backends attach the vector to an existing article, so orphans are skipped, not raised.
	if stats.Stored == 0 {
		return fmt.Errorf(
			"stored 0 embeddings: %d sent, %d skipped (no matching article), %d parse failures, %d dim mismatches\n"+
				"  • load articles before embeddings",
			stats.Sent, stats.Skipped, stats.BadIDs, stats.BadDim)
	}

	cli.Summary(out, "Embedding load complete",
		cli.IntField("sent", stats.Sent),
		cli.IntField("stored", stats.Stored),
		cli.IntField("skipped", stats.Skipped),
		cli.IntField("parse failures", stats.BadIDs),
		cli.IntField("dim mismatches", stats.BadDim),
		cli.Field{Label: "duration", Value: time.Since(start).Round(time.Millisecond).String()},
	)
	if stats.Skipped > 0 {
		cli.Warn(out, fmt.Sprintf("%d skipped — no matching article", stats.Skipped))
	}
	return nil
}

// embedIngestStats separates rows sent from rows the indexer confirmed it wrote.
type embedIngestStats struct {
	Sent    int
	Stored  int
	Skipped int
	BadIDs  int
	BadDim  int
}

// recordReader is the slice of *embedfile.Reader the ingest needs, so the batch
// and counting logic can be exercised without a Parquet file.
type recordReader interface {
	Read(records []embedfile.Record) (int, error)
}

type embedIngest struct {
	Reader    recordReader
	Indexer   storage.EmbedIndexer
	Model     string
	BatchSize int
}

func (e embedIngest) run(ctx context.Context) (stats embedIngestStats, err error) {
	buf := make([]embedfile.Record, e.BatchSize)
	batch := make([]*embedding.Vec, 0, e.BatchSize)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		res, err := e.Indexer.SaveBulk(ctx, batch)
		if err != nil {
			return err
		}
		stats.Sent += len(batch)
		stats.Stored += res.Stored
		stats.Skipped += res.Skipped
		slog.Info("Saved embedding batch",
			"sent", len(batch), "stored", res.Stored, "total_stored", stats.Stored)
		batch = batch[:0]
		return nil
	}

	for {
		if ctx.Err() != nil {
			return stats, ctx.Err()
		}

		n, readErr := e.Reader.Read(buf)
		for i := 0; i < n; i++ {
			rec := buf[i]
			id, parseErr := uuid.Parse(rec.ID)
			if parseErr != nil {
				stats.BadIDs++
				continue
			}
			if len(rec.Embedding) != expectedDim {
				stats.BadDim++
				continue
			}
			batch = append(batch, &embedding.Vec{
				ID:        id,
				Model:     e.Model,
				Embedding: rec.Embedding,
			})
			if len(batch) >= e.BatchSize {
				if err := flush(); err != nil {
					return stats, err
				}
			}
		}

		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return stats, readErr
		}
	}

	if err := flush(); err != nil {
		return stats, err
	}
	return stats, nil
}

// resolveFile returns a local path to the embeddings file, downloading from the
// object store when no local path is configured.
// resolvedFile is the embeddings file on disk plus the name worth showing. They
// differ on the object-store path, where the local copy is a temp file that
// identifies nothing.
type resolvedFile struct {
	Path    string
	Display string
	Cleanup func()
}

func resolveFile(ctx context.Context, cfg embedding.ObjectStoreConfig) (resolvedFile, error) {
	if cfg.LocalPath != "" {
		return resolvedFile{Path: cfg.LocalPath, Display: cfg.LocalPath, Cleanup: func() {}}, nil
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
		return resolvedFile{}, err
	}

	tmpFile, err := os.CreateTemp("", "embeddings-*.parquet")
	if err != nil {
		return resolvedFile{}, fmt.Errorf("create temp file: %w", err)
	}
	tmp := tmpFile.Name()
	_ = tmpFile.Close()
	cleanup := func() { _ = os.Remove(tmp) }

	slog.Info("Downloading embeddings file", "bucket", cfg.Bucket, "key", cfg.Key, "dst", tmp)
	n, err := client.Download(ctx, cfg.Key, tmp)
	if err != nil {
		cleanup()
		return resolvedFile{}, err
	}
	slog.Info("Downloaded embeddings file", "bytes", n)

	return resolvedFile{
		Path:    tmp,
		Display: fmt.Sprintf("s3://%s/%s", cfg.Bucket, cfg.Key),
		Cleanup: cleanup,
	}, nil
}
