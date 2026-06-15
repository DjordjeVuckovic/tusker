package main

import (
	"context"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/bench/judgment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStrategy implements judgment.Strategy + ModelIdentifier so we can drive
// checkResumeCompat without a real LLM/CLI backend.
type fakeStrategy struct {
	name    string
	modelID string
}

func (f fakeStrategy) Name() string { return f.name }
func (f fakeStrategy) Grade(context.Context, judgment.GradingQuery, judgment.GradingDoc) (int, error) {
	return 0, nil
}
func (f fakeStrategy) ModelID() string { return f.modelID }

func TestCheckResumeCompat_InterruptedFileResumes(t *testing.T) {
	// An interrupted run leaves Strategy set but an empty meta block.
	prior := &judgment.File{Strategy: "claude-cli"}
	strat := fakeStrategy{name: "claude-cli", modelID: "claude"}

	err := checkResumeCompat(prior, strat)
	assert.NoError(t, err, "interrupted file with empty meta should be resumable")
}

func TestCheckResumeCompat_StrategyMismatch(t *testing.T) {
	prior := &judgment.File{Strategy: "lexical"}
	strat := fakeStrategy{name: "claude-cli", modelID: "claude"}

	err := checkResumeCompat(prior, strat)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strategy mismatch")
}

func TestCheckResumeCompat_ModelMismatch(t *testing.T) {
	// A finalized file records its model — resuming with a different one is rejected.
	prior := &judgment.File{Strategy: "claude-api"}
	prior.Meta.JudgeModel = "claude-haiku-4-5"
	strat := fakeStrategy{name: "claude-api", modelID: "claude-opus-4"}

	err := checkResumeCompat(prior, strat)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model mismatch")
}

func TestCheckResumeCompat_PromptVersionMismatch(t *testing.T) {
	prior := &judgment.File{Strategy: "claude-cli"}
	prior.Meta.JudgeModel = "claude"
	prior.Meta.JudgePromptVersion = "v0-old"
	strat := fakeStrategy{name: "claude-cli", modelID: "claude"}

	err := checkResumeCompat(prior, strat)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt version mismatch")
}

func TestCheckResumeCompat_FinalizedFileMatches(t *testing.T) {
	prior := &judgment.File{Strategy: "claude-cli"}
	prior.Meta.JudgeModel = "claude"
	prior.Meta.JudgePromptVersion = judgment.PromptVersion
	strat := fakeStrategy{name: "claude-cli", modelID: "claude"}

	err := checkResumeCompat(prior, strat)
	assert.NoError(t, err)
}
