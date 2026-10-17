package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/bench/judgment"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gradedTrack(t *testing.T) (lineageTrack, uuid.UUID) {
	t.Helper()
	tr := newLineageTrack(t, "http://unused.invalid")
	doc := uuid.New()
	tr.writeJudgments(t, "2026-10-17T10-00-00-pool-aaaaaa", map[string]map[uuid.UUID]int{"q1": {doc: 3}})
	return tr, doc
}

func TestExportQrels_AnnotationsPathAsStrategyWritesQrelsBesideIt(t *testing.T) {
	tr, doc := gradedTrack(t)

	_, err := execute(t, newExportCmd(), tr.dir, "--format", "qrels", "--strategy", tr.judgments)
	require.NoError(t, err)

	_, err = judgment.ReadFile(tr.judgments)
	require.NoError(t, err, "the annotations read must survive the export")
	qrels, err := os.ReadFile(filepath.Join(tr.dir, "trec", "qrels.fixture.tsv"))
	require.NoError(t, err)
	assert.Equal(t, "q1\t0\t"+doc.String()+"\t3\n", string(qrels))
}

func TestExportQrels_RefusesToWriteOverItsInput(t *testing.T) {
	tr, _ := gradedTrack(t)
	before, err := os.ReadFile(tr.judgments)
	require.NoError(t, err)
	sameFileViaSymlinks, err := filepath.EvalSymlinks(tr.judgments)
	require.NoError(t, err)

	_, err = execute(t, newExportCmd(), tr.dir, "--format", "qrels",
		"--judgments", tr.judgments, "--output", sameFileViaSymlinks)

	require.Error(t, err)
	after, err := os.ReadFile(tr.judgments)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

// A reader holding the previous qrels keeps a whole file: the export replaces
// it rather than rewriting it in place.
func TestExportQrels_ReplacesTheOldFileWhole(t *testing.T) {
	tr, doc := gradedTrack(t)
	out := filepath.Join(tr.dir, "trec", "qrels.fixture.tsv")
	require.NoError(t, os.WriteFile(out, []byte("q1\t0\told\t1\n"), 0o644))
	held := filepath.Join(t.TempDir(), "held.tsv")
	require.NoError(t, os.Link(out, held))

	_, err := execute(t, newExportCmd(), tr.dir, "--format", "qrels", "--judgments", tr.judgments, "--output", out)
	require.NoError(t, err)

	heldContent, err := os.ReadFile(held)
	require.NoError(t, err)
	assert.Equal(t, "q1\t0\told\t1\n", string(heldContent))
	newContent, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "q1\t0\t"+doc.String()+"\t3\n", string(newContent))
}

func TestExportQrels_JudgmentsPathNamesTheDefaultOutput(t *testing.T) {
	tr, doc := gradedTrack(t)

	_, err := execute(t, newExportCmd(), tr.dir, "--format", "qrels", "--judgments", tr.judgments)
	require.NoError(t, err)

	written, err := filepath.Glob(filepath.Join(tr.dir, "trec", "*.tsv"))
	require.NoError(t, err)
	require.Len(t, written, 1, "one export writes one qrels file")
	assert.Equal(t, "qrels.fixture.tsv", filepath.Base(written[0]))
	qrels, err := os.ReadFile(written[0])
	require.NoError(t, err)
	assert.Equal(t, "q1\t0\t"+doc.String()+"\t3\n", string(qrels))
}
