package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/bench/report"
	"github.com/google/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExport_UnknownFormatListsRun(t *testing.T) {
	err := executeExport(newExportCmd(), exportFlags{format: "bogus"}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "run")
}

func TestExport_HelpDescribesRunFormat(t *testing.T) {
	cmd := newExportCmd()

	assert.Contains(t, cmd.Long, "--format run")
	assert.Contains(t, cmd.Flags().Lookup("format").Usage, "run")
}

func TestExport_RunWritesTheLatestReportsRankingsUnderItsRunID(t *testing.T) {
	reports := scaffoldDiffTrack(t)
	first, second := uuid.MustParse("00000000-0000-0000-0000-000000000001"), uuid.MustParse("00000000-0000-0000-0000-000000000002")
	rpt := diffReport(diffRun{runID: "2026-01-01T00-00-01-run-aaaaaa", kValues: []int{10}, ndcg: map[int]float64{10: 0.6}, threshold: 1})
	rpt.Jobs[0].PerQuery[0].Ranking = []report.RankedDoc{{DocID: first, Score: 2}, {DocID: second, Score: 1}}
	reportPath := filepath.Join(reports, rpt.Provenance.RunID+".json")
	require.NoError(t, report.WriteJSON(rpt, reportPath))
	require.NoError(t, updateLatestPointer(filepath.Join(reports, "latest.json"), reportPath))

	cmd := newExportCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"demo", "--format", "run"})
	require.NoError(t, cmd.Execute())

	run, err := os.ReadFile(filepath.Join(reports, "2026-01-01T00-00-01-run-aaaaaa.run", "fts.pg.run"))
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(run)), "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, []string{"q1", "Q0", first.String(), "1"}, strings.Fields(lines[0])[:4])
	assert.Equal(t, []string{"q1", "Q0", second.String(), "2"}, strings.Fields(lines[1])[:4])
	assert.Equal(t, "pg", strings.Fields(lines[0])[5])
}
