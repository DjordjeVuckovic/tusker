package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/bench/meta"
	"github.com/DjordjeVuckovic/tusker/internal/bench/report"
	"github.com/DjordjeVuckovic/tusker/internal/bench/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeCleanReport(t *testing.T, tr lineageTrack, runID string, incomplete ...string) string {
	t.Helper()
	path := filepath.Join(tr.dir, "reports", runID+".json")
	rpt := &report.Report{
		SchemaVersion: version.SchemaVersion,
		Provenance:    meta.Meta{RunID: runID},
		Settings:      report.RunSettings{Incomplete: incomplete},
	}
	require.NoError(t, report.WriteJSON(rpt, path))
	return path
}

func TestClean_KeepsLatestAndDropsIncompleteRunsFirst(t *testing.T) {
	tests := []struct {
		name     string
		keep     int
		wantKept []string
	}{
		{name: "keep two", keep: 2, wantKept: []string{"r2-latest", "r4-complete"}},
		{name: "keep three", keep: 3, wantKept: []string{"r1-complete", "r2-latest", "r4-complete"}},
		{name: "keep four", keep: 4, wantKept: []string{"r1-complete", "r2-latest", "r4-complete", "r5-latency-only"}},
		{name: "keep none", keep: 0, wantKept: []string{"r2-latest"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := newLineageTrack(t, "http://unused.invalid")
			paths := map[string]string{
				"r1-complete":     writeCleanReport(t, tr, "2026-10-01T00-00-00-run-r1-complete"),
				"r2-latest":       writeCleanReport(t, tr, "2026-10-02T00-00-00-run-r2-latest"),
				"r3-errored":      writeCleanReport(t, tr, "2026-10-03T00-00-00-run-r3-errored", "1 (query, engine) executions errored"),
				"r4-complete":     writeCleanReport(t, tr, "2026-10-04T00-00-00-run-r4-complete"),
				"r5-latency-only": writeCleanReport(t, tr, "2026-10-05T00-00-00-run-r5-latency-only", "latency-only run"),
			}
			require.NoError(t, updateLatestPointer(tr.latestPath(), paths["r2-latest"]))

			_, err := execute(t, newCleanCmd(), tr.dir, "--keep", strconv.Itoa(tt.keep))
			require.NoError(t, err)

			var kept []string
			for name, path := range paths {
				if _, err := os.Stat(path); err == nil {
					kept = append(kept, name)
				}
			}
			assert.ElementsMatch(t, tt.wantKept, kept)
			assert.FileExists(t, tr.latestPath())
		})
	}
}
