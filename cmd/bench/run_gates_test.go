package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const judgedPoolRunID = "2026-10-17T10-00-00-pool-aaaaaa"

// judgedTrack is a track whose judgments grade every doc in its pool, served by
// engines that answer every query unless failing says otherwise.
func judgedTrack(t *testing.T, failing map[string]bool) lineageTrack {
	tr, _, _ := judgedTrackDocs(t, failing)
	return tr
}

func judgedTrackDocs(t *testing.T, failing map[string]bool) (tr lineageTrack, relevant, other uuid.UUID) {
	t.Helper()
	relevant, other = uuid.New(), uuid.New()
	cluster := fakeCluster(t, map[string]*fakeIndex{
		"a": {count: 10, ranked: map[string][]uuid.UUID{"q1": {relevant, other}, "q2": {relevant}}},
		"b": {count: 10, ranked: map[string][]uuid.UUID{"q1": {other, relevant}, "q2": {other}}, failing: failing},
	})
	tr = newLineageTrack(t, cluster)
	tr.writePool(t, judgedPoolRunID, map[string][]uuid.UUID{"q1": {relevant, other}, "q2": {relevant, other}})
	tr.writeJudgments(t, judgedPoolRunID, map[string]map[uuid.UUID]int{
		"q1": {relevant: 3, other: 0},
		"q2": {relevant: 2, other: 0},
	})
	return tr, relevant, other
}

func TestRun_CompleteRunMovesLatest(t *testing.T) {
	tr := judgedTrack(t, nil)

	_, err := execute(t, newRunCmd(), tr.dir)

	require.NoError(t, err)
	require.Len(t, tr.reports(t), 1)
	latest := tr.latestReport(t)
	require.NotNil(t, latest)
	assert.Equal(t, tr.reports(t)[0].Provenance.RunID, latest.Provenance.RunID)
}

func TestRun_MissingDefaultJudgmentsFailsBeforeRunning(t *testing.T) {
	tr := judgedTrack(t, nil)
	require.NoError(t, os.Remove(tr.judgments))

	_, err := execute(t, newRunCmd(), tr.dir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--latency-only")
	assert.Empty(t, tr.reports(t))
	assert.NoFileExists(t, tr.latestPath(), "latest.json must not move")
}

func TestRun_LatencyOnlyWritesAMarkedReportAndLeavesLatest(t *testing.T) {
	tr := judgedTrack(t, nil)
	require.NoError(t, os.Remove(tr.judgments))

	_, err := execute(t, newRunCmd(), tr.dir, "--latency-only")

	require.NoError(t, err)
	reports := tr.reports(t)
	require.Len(t, reports, 1)
	assert.True(t, reports[0].Settings.LatencyOnly)
	assert.NotEmpty(t, reports[0].Settings.Incomplete)
	assert.NoFileExists(t, tr.latestPath(), "latest.json must not move")
}

func TestRun_LatencyOnlyRejectsExplicitJudgments(t *testing.T) {
	tr := judgedTrack(t, nil)

	_, err := execute(t, newRunCmd(), tr.dir, "--latency-only", "--judgments", tr.judgments)

	require.Error(t, err)
	assert.Empty(t, tr.reports(t))
}

func TestRun_FailedRunWritesTheReportButLeavesLatest(t *testing.T) {
	tests := []struct {
		name    string
		failing map[string]bool
		regrade func(relevant, other uuid.UUID) map[string]map[uuid.UUID]int
		wantErr string
	}{
		{name: "an engine errored on a query", failing: map[string]bool{"q2": true}, wantErr: "q2"},
		{
			name: "a pooled doc the judge failed to grade",
			regrade: func(relevant, other uuid.UUID) map[string]map[uuid.UUID]int {
				return map[string]map[uuid.UUID]int{"q1": {relevant: 3, other: -1}, "q2": {relevant: 2, other: 0}}
			},
			wantErr: "1 of 4 pooled docs",
		},
		{
			name: "a pooled query the judge never reached",
			regrade: func(relevant, other uuid.UUID) map[string]map[uuid.UUID]int {
				return map[string]map[uuid.UUID]int{"q1": {relevant: 3, other: 0}}
			},
			wantErr: "2 of 4 pooled docs",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, relevant, other := judgedTrackDocs(t, tt.failing)
			if tt.regrade != nil {
				tr.writeJudgments(t, judgedPoolRunID, tt.regrade(relevant, other))
			}

			_, err := execute(t, newRunCmd(), tr.dir)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Contains(t, err.Error(), "latest.json")
			reports := tr.reports(t)
			require.Len(t, reports, 1, "the report is kept for inspection")
			assert.NotEmpty(t, reports[0].Settings.Incomplete)
			assert.NoFileExists(t, tr.latestPath(), "latest.json must not move")
		})
	}
}

func TestRun_LatestMovesOnlyForTheSpecsOwnRun(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantMoved bool
	}{
		{name: "spec settings", wantMoved: true},
		{name: "every job named", args: []string{"--jobs", "a-only,both"}, wantMoved: true},
		{name: "iterations equal to the spec", args: []string{"--iterations", "2"}, wantMoved: true},
		{name: "a subset of jobs", args: []string{"--jobs", "a-only"}},
		{name: "more iterations", args: []string{"--iterations", "5"}},
		{name: "more warmup", args: []string{"--warmup", "3"}},
		{name: "other cut-offs", args: []string{"--k", "1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := judgedTrack(t, nil)

			_, err := execute(t, newRunCmd(), append([]string{tr.dir}, tt.args...)...)

			require.NoError(t, err)
			reports := tr.reports(t)
			require.Len(t, reports, 1)
			if tt.wantMoved {
				assert.NotNil(t, tr.latestReport(t))
				assert.Empty(t, reports[0].Settings.Incomplete)
			} else {
				assert.NoFileExists(t, tr.latestPath(), "latest.json must not move")
				assert.NotEmpty(t, reports[0].Settings.Incomplete)
			}
		})
	}
}

func TestRun_ReportRecordsItsSettings(t *testing.T) {
	tr := judgedTrack(t, nil)

	_, err := execute(t, newRunCmd(), tr.dir, "--jobs", "a-only", "--iterations", "4")
	require.NoError(t, err)

	reports := tr.reports(t)
	require.Len(t, reports, 1)
	settings := reports[0].Settings
	assert.Equal(t, 1, settings.Warmup, "spec warmup")
	assert.Equal(t, 4, settings.Iterations)
	assert.Equal(t, []string{"a-only"}, settings.JobFilter)
	assert.Equal(t, map[string]any{"boost": float64(2)}, settings.EngineParams["a"], "a declares boost: 2 in the spec")
	assert.Equal(t, []int{1, 2}, reports[0].Config.KValues)
}

func TestRun_SpecDefaultJudgmentsPathIsRelativeToTheTrack(t *testing.T) {
	tr := judgedTrack(t, nil)
	specPath := filepath.Join(tr.dir, "spec.yaml")
	specYAML, err := os.ReadFile(specPath)
	require.NoError(t, err)
	relative := bytes.Replace(specYAML, []byte(strconv.Quote(tr.judgments)), []byte(`"trec/annotations.fixture.yaml"`), 1)
	require.NotEqual(t, specYAML, relative)
	require.NoError(t, os.WriteFile(specPath, relative, 0o644))

	_, err = execute(t, newRunCmd(), tr.dir)

	require.NoError(t, err, "the test runs from cmd/bench, so only the track folder holds trec/")
	assert.NotNil(t, tr.latestReport(t))
}
