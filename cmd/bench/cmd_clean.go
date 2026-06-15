package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DjordjeVuckovic/tusker/internal/bench/trackctx"
	"github.com/spf13/cobra"
)

type cleanFlags struct {
	trackArg string
	keep     int
	dryRun   bool
}

func newCleanCmd() *cobra.Command {
	var f cleanFlags
	cmd := &cobra.Command{
		Use:   "clean [track]",
		Short: "Remove old report artifacts, keeping the N most recent",
		Long: `Deletes old JSON and HTML report files from tracks/<name>/reports/,
keeping the --keep most-recent runs.  latest.json is never deleted.

Reports are sorted by run timestamp in their filename (lexicographic = chronological),
so the newest N are always retained.`,
		Example: `  bench clean fts_quality            # keep 5 most recent (default)
  bench clean fts_quality --keep 3
  bench clean fts_quality --dry-run  # show what would be deleted`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeClean(cmd, f, args)
		},
	}
	cmd.Flags().StringVar(&f.trackArg, "track", "", "Track name or path")
	cmd.Flags().IntVar(&f.keep, "keep", 5, "Number of most-recent reports to keep")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "Print what would be deleted without deleting")
	return cmd
}

func executeClean(cmd *cobra.Command, f cleanFlags, args []string) error {
	tr, err := trackctx.Resolve(trackctx.Inputs{TrackArg: trackArg(f.trackArg, args)})
	if err != nil {
		return err
	}

	reportsDir := filepath.Dir(tr.LatestReportPath())
	entries, err := os.ReadDir(reportsDir)
	if err != nil {
		return fmt.Errorf("read reports dir %s: %w", reportsDir, err)
	}

	// Collect report files by extension, excluding latest.json (the pointer).
	var jsons, htmls []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "latest.json" {
			continue
		}
		switch strings.ToLower(filepath.Ext(name)) {
		case ".json":
			jsons = append(jsons, filepath.Join(reportsDir, name))
		case ".html", ".md":
			htmls = append(htmls, filepath.Join(reportsDir, name))
		}
	}

	// Sort descending (newest first by lexicographic name).
	sort.Slice(jsons, func(i, j int) bool { return jsons[i] > jsons[j] })
	sort.Slice(htmls, func(i, j int) bool { return htmls[i] > htmls[j] })

	toDelete := collectStale(jsons, f.keep)
	toDelete = append(toDelete, collectStale(htmls, f.keep)...)

	if len(toDelete) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "%s Nothing to clean in %s\n",
			cDim.Sprint("✓"), reportsDir)
		return nil
	}

	w := cmd.OutOrStdout()
	for _, path := range toDelete {
		if f.dryRun {
			fmt.Fprintf(w, "%s %s\n", cDim.Sprint("would delete"), filepath.Base(path))
			continue
		}
		if err := os.Remove(path); err != nil {
			fmt.Fprintf(w, "%s %s: %v\n", cFail.Sprint("ERR"), filepath.Base(path), err)
			continue
		}
		fmt.Fprintf(w, "%s %s\n", cOK.Sprint("deleted"), filepath.Base(path))
	}

	if f.dryRun {
		fmt.Fprintf(w, "\n%s Pass without --dry-run to delete.\n", cDim.Sprint("Dry run:"))
	} else {
		printDone(w, fmt.Sprintf("Cleaned %d file(s) from %s", len(toDelete), reportsDir))
	}
	return nil
}

// collectStale returns all items after index keep (i.e. the ones to delete).
func collectStale(sorted []string, keep int) []string {
	if keep < 0 || len(sorted) <= keep {
		return nil
	}
	return sorted[keep:]
}
