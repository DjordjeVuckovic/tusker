package main

import (
	"context"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/bench/judgment"
	"github.com/DjordjeVuckovic/tusker/internal/bench/meta"
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
