package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/bench/judgment"
	"github.com/DjordjeVuckovic/tusker/internal/bench/meta"
	"github.com/DjordjeVuckovic/tusker/internal/bench/runner"
	"github.com/DjordjeVuckovic/tusker/internal/bench/spec"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadJudgmentsMap_EmptyPath(t *testing.T) {
	m, jm, err := loadJudgmentsMap("", true)
	require.NoError(t, err)
	assert.Nil(t, m, "empty path is always silent regardless of explicit")
	assert.Nil(t, jm)
}

func TestLoadJudgmentsMap_MissingFileExplicitErrors(t *testing.T) {
	_, _, err := loadJudgmentsMap(filepath.Join(t.TempDir(), "nope.yaml"), true)
	require.Error(t, err, "explicit --judgments path must error when file missing")
	assert.Contains(t, err.Error(), "--judgments file not found")
	assert.Contains(t, err.Error(), "lexical", "error should list valid strategies")
}

func TestLoadJudgmentsMap_MissingFileImplicitSilent(t *testing.T) {
	m, _, err := loadJudgmentsMap(filepath.Join(t.TempDir(), "nope.yaml"), false)
	require.NoError(t, err, "spec.defaults missing file should NOT error — runner reports it")
	assert.Nil(t, m)
}

func TestLoadJudgmentsMap_FlattensAndFiltersUnjudged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ann.yaml")
	docOK := uuid.New()
	docSkip := uuid.New()

	jf := &judgment.File{
		Meta: meta.New("judge"),
		Queries: []judgment.Entry{
			{
				QueryID: "q1",
				Docs: []judgment.GradedDoc{
					{DocID: docOK, Grade: 2},
					{DocID: docSkip, Grade: -1},
				},
			},
		},
	}
	require.NoError(t, judgment.WriteFile(jf, path))

	m, jm, err := loadJudgmentsMap(path, true)
	require.NoError(t, err)
	require.NotNil(t, jm, "the grader identity must come back so the report can record it")
	assert.Equal(t, jf.Meta.RunID, jm.RunID)
	require.Contains(t, m, "q1")
	assert.Equal(t, 2, m["q1"][docOK.String()])
	_, present := m["q1"][docSkip.String()]
	assert.False(t, present, "unjudged (-1) entries must be filtered out")
}

func TestLoadJudgmentsMap_RejectsMissingSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ann.yaml")
	// Write a syntactically valid YAML but with no schema_version — the
	// judgment loader should reject it.
	require.NoError(t, os.WriteFile(path, []byte(`strategy: lexical
queries: []
`), 0644))

	_, _, err := loadJudgmentsMap(path, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema_version")
}

func TestNewRunConfig_MeasuresOneQueryAndOneEngineAtATime(t *testing.T) {
	cfg := newRunConfig(runFlags{}, &spec.BenchSpec{}, []int{10}, false)

	assert.Equal(t, runner.QueryParallelismSerial, cfg.QueryParallelism,
		"overlapping queries contend for the same engine")
	assert.Equal(t, runner.EngineParallelismSerial, cfg.EngineParallelism,
		"a zero here makes the runner fan out to every engine in the job, "+
			"so one engine's warmup overlaps another's measured iterations")
}

func TestNewRunConfig_FlagBeatsSpecDefault(t *testing.T) {
	bs := &spec.BenchSpec{}
	bs.Metrics.MaxK = 100
	bs.Runs.Warmup = 1
	bs.Runs.Iterations = 3

	tests := []struct {
		name       string
		flags      runFlags
		wantMaxK   int
		wantWarmup int
		wantRuns   int
	}{
		{"spec defaults when flags unset", runFlags{}, 100, 1, 3},
		{"flags win when set", runFlags{maxK: 50, warmup: 2, iters: 10}, 50, 2, 10},
		{"partial flags fall back per field", runFlags{iters: 10}, 100, 1, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newRunConfig(tt.flags, bs, []int{10}, false)
			assert.Equal(t, tt.wantMaxK, cfg.MaxK)
			assert.Equal(t, tt.wantWarmup, cfg.WarmupRuns)
			assert.Equal(t, tt.wantRuns, cfg.Runs)
		})
	}
}

func TestNewRunConfig_SpecKValuesLoseToAnExplicitFlag(t *testing.T) {
	bs := &spec.BenchSpec{}
	bs.Metrics.KValues = []int{3, 5, 10}

	fromSpec := newRunConfig(runFlags{}, bs, []int{20}, false)
	assert.Equal(t, []int{3, 5, 10}, fromSpec.KValues, "spec k_values apply when --k was not passed")

	fromFlag := newRunConfig(runFlags{}, bs, []int{20}, true)
	assert.Equal(t, []int{20}, fromFlag.KValues, "--k must override the spec")
}
