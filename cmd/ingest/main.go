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
	cliName  = "ingest"
	cliShort = "Load news datasets and embeddings into the search backend"
	cliLong  = `ingest loads data into the configured storage backend (Postgres or Elasticsearch).

Subcommands:
  articles     map and index a news dataset into the articles store
               (optionally generating embeddings inline via Ollama)
  embeddings   load precomputed embeddings (Parquet) from a file or object
               store into the article_embeddings store
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
		newArticlesCmd(),
		newEmbeddingsCmd(),
	)

	if err := root.ExecuteContext(ctx); err != nil {
		slog.Error("ingest failed", "error", err)
		os.Exit(1)
	}
}
