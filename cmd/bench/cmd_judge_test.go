package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/bench/judgment"
	"github.com/DjordjeVuckovic/tusker/internal/bench/meta"
	"github.com/DjordjeVuckovic/tusker/internal/bench/pool"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStrategy implements judgment.Strategy so we can drive checkResumeCompat
// without a real LLM/CLI backend.
type fakeStrategy struct {
	judge meta.Judge
}

func (f fakeStrategy) Name() string         { return f.judge.Strategy }
func (f fakeStrategy) Describe() meta.Judge { return f.judge }
func (f fakeStrategy) Grade(context.Context, judgment.GradingQuery, judgment.GradingDoc) (int, error) {
	return 0, nil
}

func llmJudge(provider, model string) meta.Judge {
	return meta.Judge{
		Strategy:      string(judgment.StrategyLLM),
		Provider:      provider,
		Model:         model,
		PromptVersion: judgment.PromptVersion,
	}
}

func fileWith(j meta.Judge) *judgment.File {
	return &judgment.File{Meta: meta.Meta{Judge: &j}}
}

func TestCheckResumeCompat_FileWithoutJudgeResumes(t *testing.T) {
	// A file carrying no judge block at all has nothing to contradict.
	prior := &judgment.File{}
	strat := fakeStrategy{judge: llmJudge("claude-cli", judgment.DefaultJudgeModel)}

	err := checkResumeCompat(prior, strat)
	assert.NoError(t, err, "file with no judge block should be resumable")
}

func TestCheckResumeCompat_IdenticalJudgeMatches(t *testing.T) {
	j := llmJudge("claude-cli", judgment.DefaultJudgeModel)

	err := checkResumeCompat(fileWith(j), fakeStrategy{judge: j})
	assert.NoError(t, err)
}

func TestCheckResumeCompat_StrategyMismatch(t *testing.T) {
	prior := fileWith(meta.Judge{Strategy: string(judgment.StrategyLexical)})
	strat := fakeStrategy{judge: llmJudge("claude-cli", judgment.DefaultJudgeModel)}

	err := checkResumeCompat(prior, strat)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "judge mismatch")
}

func TestCheckResumeCompat_ModelMismatch(t *testing.T) {
	prior := fileWith(llmJudge("claude-cli", "claude-haiku-4-5-20251001"))
	strat := fakeStrategy{judge: llmJudge("claude-cli", "claude-opus-5")}

	err := checkResumeCompat(prior, strat)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "judge mismatch")
}

// A provider swap at the same strategy and model must be caught too — the case
// the old flat judge_model field could not express.
func TestCheckResumeCompat_ProviderMismatch(t *testing.T) {
	prior := fileWith(llmJudge("claude-cli", "some-model"))
	strat := fakeStrategy{judge: llmJudge("codex-cli", "some-model")}

	err := checkResumeCompat(prior, strat)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "judge mismatch")
}

func TestCheckResumeCompat_PromptVersionMismatch(t *testing.T) {
	prior := fileWith(meta.Judge{
		Strategy:      string(judgment.StrategyLLM),
		Provider:      "claude-cli",
		Model:         judgment.DefaultJudgeModel,
		PromptVersion: "v0-old",
	})
	strat := fakeStrategy{judge: llmJudge("claude-cli", judgment.DefaultJudgeModel)}

	err := checkResumeCompat(prior, strat)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "judge mismatch")
}

// Legacy annotations (flat strategy/judge_model, no judge block) normalize to a
// judge whose strategy is the old selector, which cannot equal the new
// llm/claude-cli shape — so resuming onto them is correctly refused. Those
// grades came from an unrecorded model and must not be extended.
func TestCheckResumeCompat_LegacyFileRefusesResume(t *testing.T) {
	prior := &judgment.File{Meta: meta.Meta{
		LegacyStrategy:   "claude-cli",
		LegacyJudgeModel: "claude",
	}}
	prior.Meta.Normalize()
	strat := fakeStrategy{judge: llmJudge("claude-cli", judgment.DefaultJudgeModel)}

	err := checkResumeCompat(prior, strat)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "judge mismatch")
}

func judgmentsWithGrades(grades ...int) *judgment.File {
	docs := make([]judgment.GradedDoc, len(grades))
	for i, g := range grades {
		docs[i] = judgment.GradedDoc{DocID: uuid.New(), Grade: g}
	}
	return &judgment.File{
		Meta:    meta.Meta{Judge: &meta.Judge{Strategy: string(judgment.StrategyBM25)}},
		Queries: []judgment.Entry{{QueryID: "q1", Docs: docs}},
	}
}

// A file in which every grade failed must still say so: an absent count reads
// the same as a file that was never counted.
func TestWriteJudgments_RecordsCountsWhenNothingWasGraded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "annotations.bm25.yaml")

	require.NoError(t, writeJudgments(judgmentsWithGrades(judgment.GradeUnjudged, judgment.GradeUnjudged), path))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "graded_count: 0")
	assert.Contains(t, string(raw), "unjudged_count: 2")
}

func TestWriteJudgments_RecordsZeroUnjudged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "annotations.bm25.yaml")

	require.NoError(t, writeJudgments(judgmentsWithGrades(judgment.GradeRelevant), path))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "unjudged_count: 0")
}

func TestRequireJudged(t *testing.T) {
	cases := []struct {
		name          string
		grades        []int
		allowUnjudged bool
		wantErr       bool
	}{
		{name: "every doc graded", grades: []int{0, 3}},
		{name: "an unjudged doc fails", grades: []int{2, judgment.GradeUnjudged}, wantErr: true},
		{name: "an unjudged doc is allowed on request", grades: []int{2, judgment.GradeUnjudged}, allowUnjudged: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := requireJudged(judgmentsWithGrades(tc.grades...), tc.allowUnjudged)

			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// A heuristic file written under another rubric must not be extended: its
// grades and this run's would follow different rules under one judge block.
func TestCheckResumeCompat_RubricVersionMismatch(t *testing.T) {
	prior := fileWith(meta.Judge{Strategy: string(judgment.StrategyBM25)})

	err := checkResumeCompat(prior, judgment.NewBM25Strategy())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "judge mismatch")
}

func TestBuildManualJudgments_KeepsHandGradesAndAddsNewPoolDocs(t *testing.T) {
	graded, placeholder, newInPool, leftThePool, newQueryDoc := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	pf := &pool.PoolFile{Queries: []pool.PoolEntry{
		{QueryID: "q1", Docs: []pool.PooledDoc{{DocID: graded}, {DocID: placeholder}, {DocID: newInPool}}},
		{QueryID: "q2", Docs: []pool.PooledDoc{{DocID: newQueryDoc}}},
	}}
	existing := &judgment.File{Queries: []judgment.Entry{{QueryID: "q1", Docs: []judgment.GradedDoc{
		{DocID: graded, Grade: judgment.GradeRelevant},
		{DocID: placeholder, Grade: judgment.GradeUnjudged},
		{DocID: leftThePool, Grade: judgment.GradeNotRelev},
	}}}}

	got := buildManualJudgments(pf, existing)

	want := map[string]map[uuid.UUID]int{
		"q1": {
			graded:      judgment.GradeRelevant,
			placeholder: judgment.GradeUnjudged,
			newInPool:   judgment.GradeUnjudged,
			leftThePool: judgment.GradeNotRelev,
		},
		"q2": {newQueryDoc: judgment.GradeUnjudged},
	}
	assert.Equal(t, want, gradesByQuery(got))
}

func TestBuildManualJudgments_WithoutExistingFileWritesPlaceholders(t *testing.T) {
	doc := uuid.New()
	pf := &pool.PoolFile{Queries: []pool.PoolEntry{{QueryID: "q1", Docs: []pool.PooledDoc{{DocID: doc}}}}}

	got := buildManualJudgments(pf, nil)

	assert.Equal(t, map[string]map[uuid.UUID]int{"q1": {doc: judgment.GradeUnjudged}}, gradesByQuery(got))
}

func gradesByQuery(jf *judgment.File) map[string]map[uuid.UUID]int {
	out := map[string]map[uuid.UUID]int{}
	for _, qe := range jf.Queries {
		out[qe.QueryID] = map[uuid.UUID]int{}
		for _, d := range qe.Docs {
			out[qe.QueryID][d.DocID] = d.Grade
		}
	}
	return out
}
