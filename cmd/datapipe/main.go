package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

const (
	cliName  = "datapipe"
	cliShort = "News-hunter data pipeline: preprocess, load articles, load embeddings"
	cliLong  = `datapipe moves news data through the ingestion pipeline into the configured
storage backend (Postgres or Elasticsearch).

Stages:
  preprocess           clean + map a raw dataset into a canonical JSONL file
  load articles        map and index a dataset into the articles store
                       (optionally generating embeddings inline via Ollama)
  load embeddings      load precomputed embeddings (Parquet) from a file or
                       object store into the article_embeddings store

Typical flow:
  datapipe preprocess --input raw.csv --output dataset/canonical --mapping m.yaml
  datapipe load articles
  datapipe load embeddings
`
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	root := &cobra.Command{
		Use:           cliName,
		Short:         cliShort,
		Long:          cliLong,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		newPreprocessCmd(),
		newLoadCmd(),
	)

	if err := root.ExecuteContext(ctx); err != nil {
		slog.Error("datapipe failed", "error", err)
		os.Exit(1)
	}
}
