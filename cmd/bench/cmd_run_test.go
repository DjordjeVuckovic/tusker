package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/bench/judgment"
	"github.com/DjordjeVuckovic/tusker/internal/bench/meta"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadJudgmentsMap_EmptyPath(t *testing.T) {
	m, err := loadJudgmentsMap("", true)
	require.NoError(t, err)
	assert.Nil(t, m, "empty path is always silent regardless of explicit")
}

func TestLoadJudgmentsMap_MissingFileExplicitErrors(t *testing.T) {
	_, err := loadJudgmentsMap(filepath.Join(t.TempDir(), "nope.yaml"), true)
	require.Error(t, err, "explicit --judgments path must error when file missing")
	assert.Contains(t, err.Error(), "--judgments file not found")
	assert.Contains(t, err.Error(), "lexical", "error should list valid strategies")
}

func TestLoadJudgmentsMap_MissingFileImplicitSilent(t *testing.T) {
	m, err := loadJudgmentsMap(filepath.Join(t.TempDir(), "nope.yaml"), false)
	require.NoError(t, err, "spec.defaults missing file should NOT error — runner reports it")
	assert.Nil(t, m)
}

func TestLoadJudgmentsMap_FlattensAndFiltersUnjudged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ann.yaml")
	docOK := uuid.New()
	docSkip := uuid.New()

	jf := &judgment.File{
		Meta:     meta.New("judge"),
		Strategy: "lexical",
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

	m, err := loadJudgmentsMap(path, true)
	require.NoError(t, err)
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

	_, err := loadJudgmentsMap(path, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema_version")
}
