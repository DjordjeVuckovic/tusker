package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/embedding"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/types/document"
)

const defaultBatchSize = 1_000

// Pipeline defines the interface for data processing pipelines
type Pipeline interface {
	Run(ctx context.Context) error

	Stop()
}

// BulkOptions defines bulk processing configuration
type BulkOptions struct {
	Enabled bool
	Size    int
}

// PipelineConfig defines configuration for pipelines
type PipelineConfig struct {
	Name string
	Bulk *BulkOptions
}

// ArticlePipeline handles article processing from collection to storage
type ArticlePipeline struct {
	config *PipelineConfig

	collector Collector[document.Article]

	storer storage.Indexer

	embedder     *embedding.Embedder
	embedIndexer storage.EmbedIndexer
}

type PipelineOption func(pipeline *ArticlePipeline)

// WithBulk configures bulk processing with specified batch size
func WithBulk(size int) PipelineOption {
	return func(pipeline *ArticlePipeline) {
		if pipeline.config.Bulk == nil {
			pipeline.config.Bulk = &BulkOptions{}
		}
		pipeline.config.Bulk.Enabled = true
		pipeline.config.Bulk.Size = size
	}
}

// WithConfig sets custom pipeline configuration
func WithConfig(config *PipelineConfig) PipelineOption {
	return func(pipeline *ArticlePipeline) {
		pipeline.config = config
	}
}

func WithEmbeddings(storageEmbedder storage.EmbedIndexer, embedGen *embedding.Embedder) PipelineOption {
	return func(pipeline *ArticlePipeline) {
		pipeline.embedIndexer = storageEmbedder
		pipeline.embedder = embedGen
	}
}

// NewPipeline creates a new generic article processing pipeline
func NewPipeline(c Collector[document.Article], storer storage.Indexer, opts ...PipelineOption) *ArticlePipeline {
	p := &ArticlePipeline{
		collector: c,
		storer:    storer,
		config: &PipelineConfig{
			Name: "article-pipeline",
			Bulk: &BulkOptions{
				Enabled: false,
				Size:    defaultBatchSize,
			},
		},
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Run executes the pipeline
func (p *ArticlePipeline) Run(ctx context.Context) error {
	start := time.Now()
	slog.Info("🛫 Starting pipeline run",
		"pipeline", p.config.Name,
		"bulk_enabled", p.config.Bulk.Enabled,
		"batch_size", p.config.Bulk.Size,
		"time", start,
	)

	results, err := p.collector.Collect(ctx)
	if err != nil {
		slog.Error("Error collecting articles", "error", err, "pipeline", p.config.Name)
		return err
	}

	var outcome ingestOutcome
	var runErr error
	if p.config.Bulk.Enabled {
		outcome, runErr = p.processBatch(ctx, results)
	} else {
		outcome, runErr = p.processBasic(ctx, results)
	}
	if runErr == nil {
		runErr = outcome.err()
	}

	slog.Info("Pipeline run completed",
		"pipeline", p.config.Name,
		"duration", time.Since(start),
		"processed", outcome.Processed,
		"errors", outcome.Errors,
		"error", runErr,
	)

	return runErr
}

// ingestOutcome records what a run actually persisted. Every per-record failure
// is logged and skipped, so the only way to tell a real load from one that
// stored nothing is to count.
type ingestOutcome struct {
	Processed int
	Errors    int
}

// err reports a load that persisted nothing. Without it a run whose mapping
// matches no source column finishes with a nil error over an empty table.
func (o ingestOutcome) err() error {
	if o.Processed > 0 {
		return nil
	}
	if o.Errors > 0 {
		return fmt.Errorf("stored 0 articles: all %d records failed", o.Errors)
	}
	return errors.New("stored 0 articles: the source yielded no records")
}

// saveBatch persists one batch and, when embedding is enabled, its vectors.
// The size-triggered flush and the trailing partial batch both go through here
// so they cannot drift apart.
func (p *ArticlePipeline) saveBatch(ctx context.Context, articles []document.Article) error {
	if len(articles) == 0 {
		return nil
	}
	if err := p.storer.SaveBulk(ctx, articles); err != nil {
		return fmt.Errorf("save %d articles: %w", len(articles), err)
	}
	slog.Info("Bulk articles saved successfully",
		"count", len(articles),
		"pipeline", p.config.Name,
	)
	if p.embedder == nil || p.embedIndexer == nil {
		return nil
	}
	return p.embedBatch(ctx, articles)
}

// embedBatch generates and stores vectors for articles already persisted. A
// single article that cannot be embedded is skipped rather than failing the
// load; a failure to store the batch is not.
func (p *ArticlePipeline) embedBatch(ctx context.Context, articles []document.Article) error {
	embeds := make([]*embedding.Vec, 0, len(articles))
	for _, a := range articles {
		embed, err := p.embedder.EmbedDoc(ctx, a)
		if err != nil {
			slog.Error("Error generating embedding for article",
				"error", err,
				"title", a.Title,
				"pipeline", p.config.Name,
			)
			continue
		}
		embeds = append(embeds, embed)
	}
	if len(embeds) == 0 {
		return nil
	}
	if err := p.embedIndexer.SaveBulk(ctx, embeds); err != nil {
		return fmt.Errorf("save %d embeddings: %w", len(embeds), err)
	}
	slog.Info("Vec embeddings saved successfully",
		"count", len(embeds),
		"pipeline", p.config.Name,
	)
	return nil
}

// processBasic handles individual article processing
func (p *ArticlePipeline) processBasic(ctx context.Context, results <-chan Result[document.Article]) (ingestOutcome, error) {
	var outcome ingestOutcome

	for {
		select {
		case <-ctx.Done():
			slog.Info("Pipeline context cancelled, stopping collection",
				"pipeline", p.config.Name,
				"processed", outcome.Processed,
				"errors", outcome.Errors,
			)
			return outcome, ctx.Err()
		case res, ok := <-results:
			if !ok {
				slog.Info("Collection channel closed, stopping collection",
					"pipeline", p.config.Name,
					"processed", outcome.Processed,
					"errors", outcome.Errors,
				)
				return outcome, nil
			}

			if res.Err != nil {
				slog.Error("Error collecting article", "error", res.Err, "pipeline", p.config.Name)
				outcome.Errors++
				continue
			}

			if id, err := p.storer.Save(ctx, res.Result); err != nil {
				slog.Error("Error saving article",
					"error", err,
					"pipeline", p.config.Name,
					"title", res.Result.Title,
				)
				outcome.Errors++
			} else {
				slog.Debug("Article saved successfully",
					"id", id,
					"title", res.Result.Title,
					"pipeline", p.config.Name,
				)
				outcome.Processed++
			}
		}
	}
}

// processBatch handles bulk article processing. A batch that fails to store
// aborts the run: continuing would write the remaining batches over a corpus
// already known to be incomplete, and report success at the end.
func (p *ArticlePipeline) processBatch(ctx context.Context, results <-chan Result[document.Article]) (ingestOutcome, error) {
	var articles []document.Article
	var outcome ingestOutcome

	for {
		select {
		case <-ctx.Done():
			slog.Info("Pipeline context cancelled, stopping collection",
				"pipeline", p.config.Name,
				"processed", outcome.Processed,
				"errors", outcome.Errors,
				"pending_batch", len(articles),
			)
			return outcome, ctx.Err()
		case res, ok := <-results:
			if !ok {
				if err := p.saveBatch(ctx, articles); err != nil {
					return outcome, err
				}
				outcome.Processed += len(articles)
				slog.Info("Collection channel closed, stopping collection",
					"pipeline", p.config.Name,
					"processed", outcome.Processed,
					"errors", outcome.Errors,
				)
				return outcome, nil
			}

			if res.Err != nil {
				slog.Error("Error collecting article", "error", res.Err, "pipeline", p.config.Name)
				outcome.Errors++
				continue
			}

			articles = append(articles, res.Result)

			if len(articles) >= p.config.Bulk.Size {
				if err := p.saveBatch(ctx, articles); err != nil {
					return outcome, err
				}
				outcome.Processed += len(articles)
				articles = articles[:0]
			}
		}
	}
}

// Stop gracefully stops the pipeline
func (p *ArticlePipeline) Stop() {
	slog.Info("Stopping pipeline...", "pipeline", p.config.Name)

	if p.collector != nil {
		slog.Debug("Collector stopped", "pipeline", p.config.Name)
	}

	if p.storer != nil {
		p.storer = nil
		slog.Debug("Indexer cleaned up", "pipeline", p.config.Name)
	}

	slog.Info("Pipeline stopped", "pipeline", p.config.Name)
}
