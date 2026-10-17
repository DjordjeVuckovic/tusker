package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/bench/judgment"
	"github.com/DjordjeVuckovic/tusker/internal/bench/meta"
	"github.com/DjordjeVuckovic/tusker/internal/bench/pool"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func poolWithRunID(runID string) *pool.PoolFile {
	return &pool.PoolFile{
		Meta: meta.Meta{RunID: runID},
		Queries: []pool.PoolEntry{{
			QueryID: "q1",
			Docs:    []pool.PooledDoc{{DocID: uuid.New(), Sources: []string{"pg"}}},
		}},
	}
}

func TestCheckPoolBinding(t *testing.T) {
	current := poolWithRunID("2026-10-17T10-00-00-pool-aaaaaa")

	tests := []struct {
		name          string
		judgedAgainst string
		wantErr       string
	}{
		{"same pool", current.Meta.RunID, ""},
		{"regenerated pool", "2026-10-01T10-00-00-pool-bbbbbb", "2026-10-01T10-00-00-pool-bbbbbb"},
		{"judgments predating the binding", "", "re-judge"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkPoolBinding(tt.judgedAgainst, current)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// An interrupted judge run leaves only what the incremental writer flushed, so
// the binding has to be in those flushes for --resume to accept the file.
func TestBindToPool_InterruptedRunResumesAgainstTheSamePool(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "annotations.lexical.yaml")
	judge := meta.Judge{Strategy: "lexical"}
	pf := poolWithRunID("2026-10-17T10-00-00-pool-aaaaaa")

	first := judgment.NewIncrementalWriter(outPath, judge)
	require.NoError(t, bindToPool(first, nil, pf))
	entry := judgment.Entry{QueryID: "q1", Docs: []judgment.GradedDoc{{DocID: pf.Queries[0].Docs[0].DocID, Grade: 2}}}
	require.NoError(t, first.Append(judgment.QueryProgress{}, entry))

	flushed, err := judgment.ReadFile(outPath)
	require.NoError(t, err)
	assert.Equal(t, pf.Meta.RunID, flushed.Meta.PoolRunID)

	resumed := judgment.NewIncrementalWriter(outPath, judge)
	prior, err := resumed.LoadPrior()
	require.NoError(t, err)
	assert.NoError(t, bindToPool(resumed, prior, pf))
}

func TestBindToPool_ResumeRefusesAnotherPool(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "annotations.lexical.yaml")
	judge := meta.Judge{Strategy: "lexical"}

	first := judgment.NewIncrementalWriter(outPath, judge)
	require.NoError(t, bindToPool(first, nil, poolWithRunID("2026-10-01T10-00-00-pool-old000")))
	require.NoError(t, first.Flush())

	resumed := judgment.NewIncrementalWriter(outPath, judge)
	prior, err := resumed.LoadPrior()
	require.NoError(t, err)
	err = bindToPool(resumed, prior, poolWithRunID("2026-10-17T10-00-00-pool-new000"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2026-10-01T10-00-00-pool-old000")
}

func TestBindToPool_ResumeRefusesAnUnboundFile(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "annotations.lexical.yaml")
	require.NoError(t, judgment.WriteFile(&judgment.File{Meta: meta.New("judge")}, outPath))

	resumed := judgment.NewIncrementalWriter(outPath, meta.Judge{Strategy: "lexical"})
	prior, err := resumed.LoadPrior()
	require.NoError(t, err)
	err = bindToPool(resumed, prior, poolWithRunID("2026-10-17T10-00-00-pool-aaaaaa"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "re-judge")
}

func TestRun_RefusesJudgmentsGradedAgainstAnotherPool(t *testing.T) {
	doc := uuid.New()
	cluster := fakeCluster(t, map[string]*fakeIndex{
		"a": {count: 10, ranked: map[string][]uuid.UUID{"q1": {doc}, "q2": {doc}}},
		"b": {count: 10, ranked: map[string][]uuid.UUID{"q1": {doc}, "q2": {doc}}},
	})
	tr := newLineageTrack(t, cluster)
	tr.writePool(t, "2026-10-17T10-00-00-pool-new000", map[string][]uuid.UUID{"q1": {doc}, "q2": {doc}})
	tr.writeJudgments(t, "2026-10-01T10-00-00-pool-old000", map[string]map[uuid.UUID]int{
		"q1": {doc: 2}, "q2": {doc: 2},
	})

	_, err := execute(t, newRunCmd(), tr.dir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "2026-10-01T10-00-00-pool-old000")
	assert.Empty(t, tr.reports(t), "nothing is measured against judgments of another pool")
}

func TestJudgeManual_RecordsThePoolItWasMadeFrom(t *testing.T) {
	tr := newLineageTrack(t, "http://unused.invalid")
	tr.writePool(t, "2026-10-17T10-00-00-pool-aaaaaa", map[string][]uuid.UUID{"q1": {uuid.New()}})
	outPath := filepath.Join(tr.dir, "trec", "annotations.manual.yaml")

	_, err := execute(t, newJudgeCmd(), tr.dir, "--strategy", "manual", "--output", outPath)
	require.NoError(t, err)

	jf, err := judgment.ReadFile(outPath)
	require.NoError(t, err)
	assert.Equal(t, "2026-10-17T10-00-00-pool-aaaaaa", jf.Meta.PoolRunID)
}

type poolArticles map[uuid.UUID]document.Article

func (a poolArticles) GetByIDs(_ context.Context, ids []uuid.UUID) ([]document.Article, error) {
	out := make([]document.Article, 0, len(ids))
	for _, id := range ids {
		if article, ok := a[id]; ok {
			out = append(out, article)
		}
	}
	return out, nil
}

func TestJudgeGrading_RecordsThePoolItWasMadeFrom(t *testing.T) {
	tr := newLineageTrack(t, "http://unused.invalid")
	climate, election := uuid.New(), uuid.New()
	pf := &pool.PoolFile{
		Meta: meta.Meta{RunID: "2026-10-17T10-00-00-pool-aaaaaa", Engines: []string{"a"}},
		Queries: []pool.PoolEntry{{
			QueryID:   "q1",
			QueryDesc: "climate policy",
			Docs: []pool.PooledDoc{
				{DocID: climate, Sources: []string{"a"}},
				{DocID: election, Sources: []string{"a"}},
			},
		}},
	}
	require.NoError(t, pool.WritePoolFile(pf, tr.poolPath()))
	articles := poolArticles{
		climate:  {ID: climate, Title: "Climate policy talks resume", Content: "Negotiators met on climate policy."},
		election: {ID: election, Title: "Election results", Content: "Votes were counted overnight."},
	}
	previous := articleReader
	articleReader = func(*cobra.Command, string) (storage.Reader, error) { return articles, nil }
	t.Cleanup(func() { articleReader = previous })
	outPath := filepath.Join(tr.dir, "trec", "annotations.bm25.yaml")

	_, err := execute(t, newJudgeCmd(), tr.dir, "--strategy", "bm25", "--output", outPath)
	require.NoError(t, err)

	jf, err := judgment.ReadFile(outPath)
	require.NoError(t, err)
	assert.Equal(t, pf.Meta.RunID, jf.Meta.PoolRunID)
}
