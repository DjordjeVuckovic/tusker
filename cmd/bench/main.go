package main

import (
	"log/slog"
	"os"

	"github.com/DjordjeVuckovic/tusker/internal/bench/judgment"
	"github.com/DjordjeVuckovic/tusker/internal/bench/spec"
	"github.com/spf13/cobra"
)

func init() {
	// Let the spec loader query the strategy registry without importing
	// judgment directly (keeps spec package dep-free of judgment).
	spec.KnownStrategies = judgment.KnownStrategies
}

const (
	cliName  = "bench"
	cliShort = "Search engine quality + latency benchmark"
	cliLong  = `bench evaluates full-text, vector, and hybrid search queries against multiple
engines (Postgres, ParadeDB, Elasticsearch, the tusker API), produces
IR-quality metrics (NDCG, MAP, MRR, Bpref, P/R/F1) and latency statistics.

Typical pipeline (pass the track name as a positional arg):

  1. bench init <name>              scaffold tracks/<name>/
  2. bench validate <name>          dry-run every query through each engine
  3. bench pool <name>              gather candidate docs → trec/pool.yaml
  4. bench judge <name>             grade with lexical (default) or LLM strategy
  5. bench run <name>               execute + report (reads spec.defaults.judgments)
  6. bench export <name> --format html    shareable HTML report
     bench export <name> --format qrels  TREC qrels for trec_eval / R / Python

  bench status <name>               see where you left off
  bench diff   <name>               compare latest two runs
  bench clean  <name>               remove old report files (keep N newest)

Tracks live under ./tracks as either a flat folder (fts_quality) or nested as
<dataset>/<paradigm> (news/fts). validate/pool/judge/run/status accept a glob
(news/*) to run every paradigm of a dataset at once — quote it: bench run 'news/*'.
`
)

func main() {
	root := &cobra.Command{
		Use:           cliName,
		Short:         cliShort,
		Long:          cliLong,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		newRunCmd(),
		newPoolCmd(),
		newJudgeCmd(),
		newValidateCmd(),
		newInitCmd(),
		newShowCmd(),
		newExportCmd(),
		newStatusCmd(),
		newDiffCmd(),
		newCleanCmd(),
		newReportCmd(), // top-level alias for bench show report
	)

	if err := root.Execute(); err != nil {
		slog.Error("bench failed", "error", err)
		os.Exit(1)
	}
}
