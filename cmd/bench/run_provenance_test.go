package main

import (
	"path/filepath"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/bench/judgment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun_ReportNamesThePoolAndJudgmentsItScoredAgainst(t *testing.T) {
	tr := judgedTrack(t, nil)
	jf, err := judgment.ReadFile(tr.judgments)
	require.NoError(t, err)

	_, err = execute(t, newRunCmd(), tr.dir)
	require.NoError(t, err)

	provenance := tr.latestReport(t).Provenance
	assert.Equal(t, tr.poolRunID(t), provenance.PoolRunID)
	assert.Equal(t, jf.Meta.RunID, provenance.JudgmentsRunID)
}

func TestRun_ReportListsTheSuitesItsJobsLoaded(t *testing.T) {
	tr := judgedTrack(t, nil)
	aOnlySuite := copyFile(t, filepath.Join(tr.dir, "suite.yaml"), filepath.Join(tr.dir, "suite-a.yaml"))
	editFile(t, filepath.Join(tr.dir, "spec.yaml"), "  - name: a-only\n    suite: suite.yaml", "  - name: a-only\n    suite: suite-a.yaml")

	_, err := execute(t, newRunCmd(), tr.dir)
	require.NoError(t, err)
	_, err = execute(t, newRunCmd(), tr.dir, "--jobs", "a-only")
	require.NoError(t, err)

	suitesByJobs := map[int][]string{}
	for _, rpt := range tr.reports(t) {
		suitesByJobs[len(rpt.Jobs)] = rpt.Provenance.Sources.Suites
	}
	resolved, err := filepath.EvalSymlinks(aOnlySuite)
	require.NoError(t, err)
	both := filepath.Join(filepath.Dir(resolved), "suite.yaml")
	assert.ElementsMatch(t, []string{both, resolved}, canonicalPaths(t, suitesByJobs[2]), "every job's suite")
	assert.Equal(t, []string{resolved}, canonicalPaths(t, suitesByJobs[1]), "only the suite of the job that ran")
}

func canonicalPaths(t *testing.T, paths []string) []string {
	t.Helper()
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		resolved, err := filepath.EvalSymlinks(p)
		require.NoError(t, err)
		out = append(out, resolved)
	}
	return out
}
