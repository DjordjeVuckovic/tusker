package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DjordjeVuckovic/tusker/internal/bench/judgment"
	"github.com/DjordjeVuckovic/tusker/internal/bench/report"
	"github.com/DjordjeVuckovic/tusker/internal/bench/trackctx"
	"github.com/spf13/cobra"
)

type exportFlags struct {
	trackArg      string
	format        string
	strategy      string
	judgmentsPath string
	output        string
}

func newExportCmd() *cobra.Command {
	var f exportFlags
	cmd := &cobra.Command{
		Use:   "export [track]",
		Short: "Export benchmark artifacts to TSV or HTML",
		Long: `Exports bench artifacts in a machine-readable or presentation format.

  --format qrels     TREC qrels TSV (query_id 0 doc_id grade) for trec_eval, R, Python
  --format tsv       Alias for qrels
  --format html      Self-contained HTML report with sortable tables, SVG charts,
                     significance table, and provenance block — suitable for thesis
                     appendices or sharing without tooling.
  --format markdown  GitHub-Flavored Markdown tables for thesis writing and PRs

qrels/tsv picks the judgments file in this order: --judgments PATH > --strategy NAME >
defaults to lexical. html and markdown read the track's latest report.`,
		Example: `  bench export fts_quality --format qrels
  bench export fts_quality --format qrels --strategy claude-api
  bench export fts_quality --format html
  bench export fts_quality --format markdown
  bench export fts_quality --format markdown --output /tmp/results.md
  bench export --judgments /tmp/ad-hoc.yaml --format qrels --output /tmp/q.tsv`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeExport(cmd, f, args)
		},
	}
	cmd.Flags().StringVar(&f.trackArg, "track", "", "Track name or path")
	cmd.Flags().StringVar(&f.format, "format", "qrels", "Export format: qrels (TREC TSV), html, or markdown")
	cmd.Flags().StringVar(&f.strategy, "strategy", string(judgment.StrategyLexical), "Judgment strategy for TSV export")
	cmd.Flags().StringVar(&f.judgmentsPath, "judgments", "", "Override annotations YAML path (TSV only)")
	cmd.Flags().StringVar(&f.output, "output", "", "Override output path")
	return cmd
}

func executeExport(cmd *cobra.Command, f exportFlags, args []string) error {
	switch strings.ToLower(f.format) {
	case "qrels", "tsv":
		return exportTSV(cmd, f, args)
	case "html":
		return exportHTML(cmd, f, args)
	case "markdown", "md":
		return exportMarkdown(cmd, f, args)
	default:
		return fmt.Errorf("unknown format %q (choose qrels, html, or markdown)", f.format)
	}
}

func exportTSV(cmd *cobra.Command, f exportFlags, args []string) error {
	tr, err := trackctx.Resolve(trackctx.Inputs{
		TrackArg:   trackArg(f.trackArg, args),
		OutputPath: f.output,
	})
	if err != nil {
		return err
	}

	jPath := f.judgmentsPath
	if jPath == "" {
		jPath = tr.JudgmentsPath(f.strategy)
	}

	outPath := f.output
	if outPath == "" {
		outPath = tr.QrelsPath(f.strategy)
	}

	jf, err := judgment.ReadFile(jPath)
	if err != nil {
		return fmt.Errorf("read judgments %s: %w", jPath, err)
	}
	if err := judgment.WriteQrels(jf, outPath); err != nil {
		return fmt.Errorf("write qrels: %w", err)
	}
	printDone(cmd.OutOrStdout(), "Qrels written: "+outPath)
	return nil
}

func exportMarkdown(cmd *cobra.Command, f exportFlags, args []string) error {
	tr, err := trackctx.Resolve(trackctx.Inputs{
		TrackArg: trackArg(f.trackArg, args),
	})
	if err != nil {
		return err
	}

	rpt, err := report.ReadLatestReport(tr.LatestReportPath())
	if err != nil {
		return fmt.Errorf("read latest report: %w", err)
	}

	outPath := f.output
	if outPath == "" {
		runID := rpt.Provenance.RunID
		if runID == "" {
			runID = "report"
		}
		outPath = filepath.Join(filepath.Dir(tr.LatestReportPath()), runID+".md")
	}

	f2, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create markdown file: %w", err)
	}
	defer f2.Close()

	report.WriteMarkdown(rpt, f2)
	printDone(cmd.OutOrStdout(), "Markdown report written: "+outPath)
	return nil
}

func exportHTML(cmd *cobra.Command, f exportFlags, args []string) error {
	tr, err := trackctx.Resolve(trackctx.Inputs{
		TrackArg: trackArg(f.trackArg, args),
	})
	if err != nil {
		return err
	}

	rpt, err := report.ReadLatestReport(tr.LatestReportPath())
	if err != nil {
		return fmt.Errorf("read latest report: %w", err)
	}

	outPath := f.output
	if outPath == "" {
		// Derive HTML path from the run ID: reports/<run_id>.html
		runID := rpt.Provenance.RunID
		if runID == "" {
			runID = "report"
		}
		outPath = filepath.Join(filepath.Dir(tr.LatestReportPath()), runID+".html")
	}

	if err := report.WriteHTML(rpt, outPath); err != nil {
		return fmt.Errorf("write HTML report: %w", err)
	}
	printDone(cmd.OutOrStdout(), "HTML report written: "+outPath)
	return nil
}
