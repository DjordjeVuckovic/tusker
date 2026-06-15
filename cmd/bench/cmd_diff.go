package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/bench/report"
	"github.com/DjordjeVuckovic/tusker/internal/bench/trackctx"
	"github.com/spf13/cobra"
)

type diffFlags struct {
	trackArg string
	pathA    string
	pathB    string
}

func newDiffCmd() *cobra.Command {
	var f diffFlags
	cmd := &cobra.Command{
		Use:   "diff [track]",
		Short: "Compare two benchmark runs side by side",
		Long: `Loads two report JSON files and shows per-engine metric deltas and the
per-query NDCG regressions sorted by magnitude.

Without --a/--b, picks the two most-recent reports in tracks/<name>/reports/.
With --a and --b, accepts run IDs (e.g. 2026-05-26T23-39-05-run-7e0750) or
direct paths to report.json files.`,
		Example: `  bench diff fts_quality                               # latest two runs
  bench diff fts_quality --b 2026-05-26T23-39-05-run-7e0750
  bench diff --a /tmp/before.json --b /tmp/after.json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeDiff(cmd.OutOrStdout(), f, args)
		},
	}
	cmd.Flags().StringVar(&f.trackArg, "track", "", "Track name or path")
	cmd.Flags().StringVar(&f.pathA, "a", "", "Before run: run ID or path to report.json")
	cmd.Flags().StringVar(&f.pathB, "b", "", "After run: run ID or path to report.json")
	return cmd
}

func executeDiff(w io.Writer, f diffFlags, args []string) error {
	tr, err := trackctx.Resolve(trackctx.Inputs{TrackArg: trackArg(f.trackArg, args)})
	if err != nil {
		// If both paths are explicit we don't need a track.
		if f.pathA != "" && f.pathB != "" {
			tr = nil
		} else {
			return err
		}
	}

	pathA, err := resolveReportPath(tr, f.pathA, 1 /*second-latest*/)
	if err != nil {
		return fmt.Errorf("resolve before report: %w", err)
	}
	pathB, err := resolveReportPath(tr, f.pathB, 0 /*latest*/)
	if err != nil {
		return fmt.Errorf("resolve after report: %w", err)
	}
	if pathA == pathB {
		return fmt.Errorf("both runs resolve to the same file: %s", pathA)
	}

	rptA, err := report.ReadJSON(pathA)
	if err != nil {
		return fmt.Errorf("read before report: %w", err)
	}
	rptB, err := report.ReadJSON(pathB)
	if err != nil {
		return fmt.Errorf("read after report: %w", err)
	}

	printDiff(w, rptA, rptB)
	return nil
}

// resolveReportPath turns a user flag (run ID or path) into an absolute path.
// When empty, it picks the Nth most-recent report from the track's reports dir.
func resolveReportPath(tr *trackctx.Track, flag string, nth int) (string, error) {
	if flag != "" {
		if looksLikePath(flag) {
			return flag, nil
		}
		// Treat as run ID → <reports>/<runID>.json
		if tr == nil {
			return "", fmt.Errorf("run ID %q given but no track resolved", flag)
		}
		return tr.ReportPath(flag), nil
	}
	if tr == nil {
		return "", fmt.Errorf("no path or run ID given and no track available")
	}
	return nthLatestReport(filepath.Dir(tr.LatestReportPath()), nth)
}

// nthLatestReport returns the nth most-recent report JSON in dir (0 = latest).
func nthLatestReport(dir string, nth int) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read reports dir %s: %w", dir, err)
	}
	var reports []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") && e.Name() != "latest.json" {
			reports = append(reports, filepath.Join(dir, e.Name()))
		}
	}
	if len(reports) < nth+1 {
		return "", fmt.Errorf("need at least %d report(s) in %s, found %d", nth+1, dir, len(reports))
	}
	// Sort by name descending — names are timestamped so lexicographic = chronological.
	sort.Slice(reports, func(i, j int) bool { return reports[i] > reports[j] })
	return reports[nth], nil
}

// ─── diff rendering ───────────────────────────────────────────────────────────

func printDiff(w io.Writer, a, b *report.Report) {
	fmt.Fprintf(w, "\n%s\n", cBold.Sprint("Comparing:"))
	fmt.Fprintf(w, "  %s %s\n", cDim.Sprint("A (before):"), a.Provenance.RunID)
	fmt.Fprintf(w, "  %s %s\n\n", cDim.Sprint("B (after): "), cBold.Sprint(b.Provenance.RunID))

	// Find common jobs by name.
	jobsA := indexJobs(a)
	jobsB := indexJobs(b)

	names := unionKeys(jobsA, jobsB)
	sort.Strings(names)

	kVals := a.Config.KValues
	if len(kVals) == 0 {
		kVals = b.Config.KValues
	}
	primaryKVal := primaryKFromSlice(kVals)

	for _, name := range names {
		jA, okA := jobsA[name]
		jB, okB := jobsB[name]
		if !okA || !okB {
			missing := "B"
			if !okA {
				missing = "A"
			}
			fmt.Fprintf(w, "%s\n", cDim.Sprintf("--- Job: %s (only in %s, skipped) ---", name, missing))
			continue
		}

		fmt.Fprintf(w, "%s\n\n", cBold.Sprintf("--- Job: %s ---", name))
		printAggregateDiff(w, jA, jB, kVals)
		printQueryDiff(w, jA, jB, primaryKVal)
		fmt.Fprintln(w)
	}
}

func printAggregateDiff(w io.Writer, jA, jB *report.JobReport, kVals []int) {
	aggA := indexAgg(jA)
	aggB := indexAgg(jB)
	engines := unionKeys(aggA, aggB)
	sort.Strings(engines)

	primaryKVal := primaryKFromSlice(kVals)

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "Engine\tNDCG@K  A→B\tMAP  A→B\tMRR  A→B\tp50  A→B")
	fmt.Fprintln(tw, "---\t---\t---\t---\t---")

	for _, eng := range engines {
		eA, okA := aggA[eng]
		eB, okB := aggB[eng]
		if !okA || !okB {
			fmt.Fprintf(tw, "%s\t(only in one run)\n", eng)
			continue
		}
		ndcgA, ndcgB := eA.NDCG[primaryKVal], eB.NDCG[primaryKVal]
		mapA, mapB := eA.MAP, eB.MAP
		mrrA, mrrB := eA.MRR, eB.MRR
		latA, latB := eA.Latency.P50(), eB.Latency.P50()

		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			eng,
			fmtDelta("%.4f", ndcgA, ndcgB, true),
			fmtDelta("%.4f", mapA, mapB, true),
			fmtDelta("%.4f", mrrA, mrrB, true),
			fmtDurationDelta(latA, latB), // last column — safe to color
		)
	}
	tw.Flush()
	fmt.Fprintln(w)
}

func printQueryDiff(w io.Writer, jA, jB *report.JobReport, k int) {
	type delta struct {
		query, engine string
		a, b          float64
		diff          float64
	}

	perA := indexPerQuery(jA)
	perB := indexPerQuery(jB)

	var deltas []delta
	for key, eA := range perA {
		eB, ok := perB[key]
		if !ok || !eA.Judged || !eB.Judged {
			continue
		}
		d := eB.NDCG[k] - eA.NDCG[k]
		if d == 0 {
			continue
		}
		parts := strings.SplitN(key, "\x00", 2)
		deltas = append(deltas, delta{parts[0], parts[1], eA.NDCG[k], eB.NDCG[k], d})
	}
	if len(deltas) == 0 {
		return
	}
	// Sort: biggest regression first, then biggest improvement.
	sort.Slice(deltas, func(i, j int) bool { return deltas[i].diff < deltas[j].diff })

	fmt.Fprintf(w, "%s\n\n", cBold.Sprintf("Per-Query NDCG@%d Deltas (largest regression first):", k))
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "Query\tEngine\tA\tB\tΔ")
	fmt.Fprintln(tw, "---\t---\t---\t---\t---")
	for _, d := range deltas {
		// Δ is the last column — safe to color.
		var delta string
		if d.diff > 0 {
			delta = cOK.Sprintf("↑%+.4f", d.diff)
		} else {
			delta = cFail.Sprintf("↓%+.4f", d.diff)
		}
		fmt.Fprintf(tw, "%s\t%s\t%.4f\t%.4f\t%s\n",
			d.query, d.engine, d.a, d.b, delta)
	}
	tw.Flush()
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func indexJobs(r *report.Report) map[string]*report.JobReport {
	m := make(map[string]*report.JobReport, len(r.Jobs))
	for i := range r.Jobs {
		m[r.Jobs[i].JobName] = &r.Jobs[i]
	}
	return m
}

func indexAgg(jr *report.JobReport) map[string]report.AggregatedEntry {
	m := make(map[string]report.AggregatedEntry, len(jr.Aggregated))
	for _, a := range jr.Aggregated {
		m[a.EngineName] = a
	}
	return m
}

func indexPerQuery(jr *report.JobReport) map[string]report.Entry {
	m := make(map[string]report.Entry, len(jr.PerQuery))
	for _, e := range jr.PerQuery {
		key := e.QueryID + "\x00" + e.EngineName
		m[key] = e
	}
	return m
}

func unionKeys[V any](a, b map[string]V) []string {
	seen := make(map[string]struct{})
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

func fmtDelta(format string, a, b float64, higherBetter bool) string {
	d := b - a
	if d == 0 {
		return fmt.Sprintf(format+" (=)", a)
	}
	improved := (d > 0) == higherBetter
	arrow := "↑"
	if !improved {
		arrow = "↓"
	}
	pct := 0.0
	if a != 0 {
		pct = d / a * 100
	}
	return fmt.Sprintf(format+"→"+format+" %s%+.1f%%", a, b, arrow, pct)
}

func fmtDurationDelta(a, b time.Duration) string {
	if a == 0 && b == 0 {
		return "—"
	}
	d := b - a
	if d == 0 {
		return cDim.Sprint(durStr(a) + " (=)")
	}
	// For latency: lower is better, so improvement = d < 0.
	improved := d < 0
	arrow := "↑" // up = slower = worse
	if improved {
		arrow = "↓" // down = faster = better
	}
	pct := 0.0
	if a != 0 {
		pct = float64(d) / float64(a) * 100
	}
	s := fmt.Sprintf("%s→%s %s%+.1f%%", durStr(a), durStr(b), arrow, pct)
	if improved {
		return cOK.Sprint(s)
	}
	return cFail.Sprint(s)
}

func primaryKFromSlice(kVals []int) int {
	if len(kVals) == 0 {
		return 10
	}
	return kVals[len(kVals)-1]
}

func durStr(d time.Duration) string {
	if d == 0 {
		return "-"
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.1fµs", float64(d.Microseconds()))
	}
	if d < time.Second {
		return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}
