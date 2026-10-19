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
	cliShort = "Tusker data pipeline: preprocess a dataset, load it into an engine"
	cliLong  = `datapipe moves news data from a raw dataset into one search engine.

Stages:
  preprocess           clean + map a raw dataset into a canonical JSONL file
  load                 load a canonical package, and optionally an embeddings
                       package built from it, into native, paradedb, tiger or es

Typical flow:
  datapipe preprocess --input datasets/<name>/dataset.csv \
      --output datasets/<name>/canonical.jsonl --mapping m.yaml
  datapipe load --engine native \
      --canonical datasets/<name>/canonical/<version>/datapackage.json \
      --embeddings datasets/<name>/embeddings/<tag-dir>/<version>/datapackage.json
`
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := newRootCmd().ExecuteContext(ctx); err != nil {
		slog.Error("datapipe failed", "error", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
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
	return root
}
