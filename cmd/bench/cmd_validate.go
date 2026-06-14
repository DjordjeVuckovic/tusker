package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/DjordjeVuckovic/news-hunter/internal/bench/engine"
	"github.com/DjordjeVuckovic/news-hunter/internal/bench/spec"
	"github.com/DjordjeVuckovic/news-hunter/internal/bench/suite"
	"github.com/DjordjeVuckovic/news-hunter/internal/bench/trackctx"
	"github.com/DjordjeVuckovic/news-hunter/internal/storage"
	"github.com/spf13/cobra"
)

type validateFlags struct {
	trackArg  string
	specPath  string
	suitePath string
	failFast  bool
}

func newValidateCmd() *cobra.Command {
	var f validateFlags
	cmd := &cobra.Command{
		Use:   "validate [track]",
		Short: "Dry-run every query through each engine and report broken ones",
		Long: `Validates spec + suite ahead of a real pool/run:

  - templates render with the params provided
  - postgres queries pass EXPLAIN (syntax, columns, operators)
  - elasticsearch queries pass _validate/query (JSON, fields, types)
  - api descriptors parse as {method, path, body?, params?}

Returns non-zero exit if any query fails — wire it into CI.`,
		Example: `  bench validate fts_quality
  bench validate --track tracks/fts_quality --fail-fast`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeValidate(cmd, f, args)
		},
	}
	cmd.Flags().StringVar(&f.trackArg, "track", "", "Track name or path")
	cmd.Flags().StringVar(&f.specPath, "spec", "", "Override spec.yaml path")
	cmd.Flags().StringVar(&f.suitePath, "suite", "", "Override suite.yaml path (all jobs share it)")
	cmd.Flags().BoolVar(&f.failFast, "fail-fast", false, "Stop at first failure")
	return cmd
}

type validateRow struct {
	queryID string
	engine  string
	status  string
	detail  string
}

func executeValidate(cmd *cobra.Command, f validateFlags, args []string) error {
	return forEachTrack(cmd.OutOrStdout(), trackctx.Inputs{
		TrackArg:  trackArg(f.trackArg, args),
		SpecPath:  f.specPath,
		SuitePath: f.suitePath,
	}, func(tr *trackctx.Track) error {
		return validateTrack(cmd, f, tr)
	})
}

func validateTrack(cmd *cobra.Command, f validateFlags, tr *trackctx.Track) error {
	bs, err := spec.LoadFromFile(tr.Spec)
	if err != nil {
		return fmt.Errorf("load spec: %w", err)
	}
	printSpecWarnings(cmd.OutOrStdout(), bs)

	// A semantic/hybrid track without an embedder can never resolve its vector
	// queries; fail here rather than letting every row stub a fake vector and
	// report a misleading OK.
	vectorStore, err := buildQueryVectorStore(cmd.Context(), bs)
	if err != nil {
		return fmt.Errorf("build vector store: %w", err)
	}
	if err := requireEmbedder(bs, vectorStore); err != nil {
		return err
	}

	executors, cleanup, err := createExecutors(cmd.Context(), bs)
	if err != nil {
		return fmt.Errorf("create executors: %w", err)
	}
	defer cleanup()

	var rows []validateRow
	failures := 0
	suites := map[string]*suite.LoadedSuite{}
	// seen deduplicates (suitePath, queryID, engineName) triples — two jobs
	// that share the same suite and engines would otherwise re-validate the
	// same pairs, doubling traffic and output noise.
	seen := map[string]struct{}{}

	for _, job := range bs.Jobs {
		ls, ok := suites[job.Suite]
		if !ok {
			loaded, err := suite.LoadFromFile(job.Suite)
			if err != nil {
				return fmt.Errorf("load suite for job %q: %w", job.Name, err)
			}
			suites[job.Suite] = loaded
			ls = loaded
		}
		for _, q := range ls.Suite.Queries {
			for _, engName := range job.Engines {
				key := job.Suite + "\x00" + q.ID + "\x00" + engName
				if _, done := seen[key]; done {
					continue
				}
				seen[key] = struct{}{}

				row := validateRow{queryID: q.ID, engine: engName}
				row = validateOne(cmd.Context(), row, q, engName, ls, executors[engName], vectorStore)
				rows = append(rows, row)
				if row.status != "OK" && row.status != "SKIP" {
					failures++
					if f.failFast {
						printValidateRows(cmd.OutOrStdout(), rows)
						return fmt.Errorf("validation failed (fail-fast)")
					}
				}
			}
		}
	}

	warnKindDrift(cmd.OutOrStdout(), bs, suites)

	printValidateRows(cmd.OutOrStdout(), rows)
	fmt.Fprintln(cmd.OutOrStdout())
	if failures > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "Total: %d checks  %s\n",
			len(rows), cFail.Sprintf("%d failed", failures))
		return fmt.Errorf("%d query/engine pair(s) failed validation", failures)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Total: %d checks  %s\n",
		len(rows), cOK.Sprint("all passed"))
	return nil
}

func validateOne(ctx context.Context, row validateRow, q suite.Query, engName string, ls *suite.LoadedSuite, exec engine.Executor, store storage.VectorStore) validateRow {
	var extra suite.TemplateParams
	if q.NeedsQueryVector() {
		if store != nil {
			// Embed the real query so dimensionality (a 1-dim stub vs VECTOR(1024))
			// is exercised here, not deferred to pool/run.
			vec, err := store.QueryVector(ctx, q.Description)
			if err != nil {
				row.status = "EMBED_ERR"
				row.detail = truncate(err.Error(), 120)
				return row
			}
			extra = suite.TemplateParams{suite.ReservedQueryVectorParam: suite.FormatVector(vec)}
		} else {
			// No embedder, and the kind doesn't require one — stub a placeholder so
			// the query parses, but say so rather than report a bare OK.
			extra = suite.TemplateParams{suite.ReservedQueryVectorParam: "[0]"}
			row.detail = "stubbed vector"
		}
	}
	resolved, err := q.ResolveEngineQuery(engName, ls.Registry, ls.Dir, extra)
	if err != nil {
		row.status = "TEMPLATE_ERR"
		row.detail = err.Error()
		return row
	}
	if resolved == nil {
		row.status = "SKIP"
		row.detail = "no query for this engine"
		return row
	}
	v, ok := exec.(engine.Validator)
	if !ok {
		row.status = "UNSUPPORTED"
		row.detail = "executor does not implement Validator"
		return row
	}
	if err := v.Validate(ctx, resolved.Query); err != nil {
		row.status = "INVALID"
		row.detail = truncate(err.Error(), 120)
		return row
	}
	row.status = "OK"
	return row
}

// warnKindDrift cross-checks the declared kind against observed query usage so
// the two sources of truth can't silently diverge: a semantic/hybrid kind whose
// queries never reference {{precomputed}}, or vector-bearing queries under a
// non-vector kind. Advisory only — it never fails the run.
func warnKindDrift(w io.Writer, bs *spec.BenchSpec, suites map[string]*suite.LoadedSuite) {
	anyNeedsVector := false
	for _, ls := range suites {
		for i := range ls.Suite.Queries {
			if ls.Suite.Queries[i].NeedsQueryVector() {
				anyNeedsVector = true
			}
		}
	}
	switch {
	case bs.Kind.RequiresEmbedder() && !anyNeedsVector:
		printWarn(w, fmt.Sprintf("kind %q expects vector queries, but none reference {{%s}}",
			bs.Kind, suite.ReservedQueryVectorParam))
	case bs.Kind != "" && !bs.Kind.RequiresEmbedder() && anyNeedsVector:
		printWarn(w, fmt.Sprintf("queries reference {{%s}} but kind %q is not semantic/hybrid",
			suite.ReservedQueryVectorParam, bs.Kind))
	}
}

func printValidateRows(w io.Writer, rows []validateRow) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, cBold.Sprint("QUERY")+"\t"+cBold.Sprint("ENGINE")+"\t"+cBold.Sprint("STATUS")+"\t"+cBold.Sprint("DETAIL"))
	fmt.Fprintln(tw, "-----\t------\t------\t------")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.queryID, r.engine, colorStatus(r.status), r.detail)
	}
	tw.Flush()
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
