package judgment

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DjordjeVuckovic/tusker/internal/bench/meta"
	"github.com/DjordjeVuckovic/tusker/internal/bench/pool"
)

func TestWriteAtomic_NoLeftoverTempOnSuccess(t *testing.T) {
	dir := t.TempDir()
	jf := &File{
		Meta:    meta.Meta{Judge: &meta.Judge{Strategy: "x"}},
		Queries: []Entry{{QueryID: "q"}},
	}
	path := filepath.Join(dir, "ann.yaml")

	require.NoError(t, WriteFile(jf, path))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".bench-")
		assert.NotContains(t, e.Name(), ".tmp")
	}
}

func TestReadFileIfExists_MissingReturnsNil(t *testing.T) {
	jf, err := ReadFileIfExists(filepath.Join(t.TempDir(), "nope.yaml"))
	require.NoError(t, err)
	assert.Nil(t, jf)
}

func TestIncrementalWriter_AppendFlushesEachQuery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ann.yaml")
	w := NewIncrementalWriter(path, meta.Meta{Judge: &meta.Judge{Strategy: "test-strat"}})

	docID := uuid.New()
	err := w.Append(QueryProgress{QueryID: "q1"}, Entry{
		QueryID: "q1",
		Docs:    []GradedDoc{{DocID: docID, Grade: 2}},
	})
	require.NoError(t, err)

	jf, err := ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "test-strat", jf.Judge().Strategy)
	require.Len(t, jf.Queries, 1)
	assert.Equal(t, 2, jf.Queries[0].Docs[0].Grade)

	// Append another query; existing one persists.
	err = w.Append(QueryProgress{QueryID: "q2"}, Entry{
		QueryID: "q2",
		Docs:    []GradedDoc{{DocID: uuid.New(), Grade: 3}},
	})
	require.NoError(t, err)
	jf, err = ReadFile(path)
	require.NoError(t, err)
	assert.Len(t, jf.Queries, 2)
}

func TestIncrementalWriter_AppendReplacesSameQuery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ann.yaml")
	w := NewIncrementalWriter(path, meta.Meta{Judge: &meta.Judge{Strategy: "test"}})

	id := uuid.New()
	require.NoError(t, w.Append(QueryProgress{QueryID: "q1"}, Entry{
		QueryID: "q1",
		Docs:    []GradedDoc{{DocID: id, Grade: 0}},
	}))
	require.NoError(t, w.Append(QueryProgress{QueryID: "q1"}, Entry{
		QueryID: "q1",
		Docs:    []GradedDoc{{DocID: id, Grade: 3}},
	}))

	jf, err := ReadFile(path)
	require.NoError(t, err)
	require.Len(t, jf.Queries, 1)
	assert.Equal(t, 3, jf.Queries[0].Docs[0].Grade, "second Append should overwrite same QueryID")
}

func TestIncrementalWriter_LoadPriorRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ann.yaml")

	prior := &File{
		Meta: meta.Meta{Judge: &meta.Judge{Strategy: "test"}},
		Queries: []Entry{
			{QueryID: "q1", Docs: []GradedDoc{{DocID: uuid.New(), Grade: 2}}},
		},
	}
	require.NoError(t, WriteFile(prior, path))

	w := NewIncrementalWriter(path, meta.Meta{Judge: &meta.Judge{Strategy: "test"}})
	loaded, err := w.LoadPrior()
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Len(t, loaded.Queries, 1)
}

// Grade counts describe a judgment set; pools share the meta block but have
// nothing to count.
func TestGradeCounts_StayOffPoolFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pool.yaml")
	pf := &pool.PoolFile{Meta: meta.New("pool"), Queries: []pool.PoolEntry{{QueryID: "q1", Docs: []pool.PooledDoc{{DocID: uuid.New()}}}}}

	require.NoError(t, pool.WritePoolFile(pf, path))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "graded_count")
	assert.NotContains(t, string(raw), "unjudged_count")
}

// An interrupted run leaves only intermediate flushes behind, so each must carry
// the caller's meta for a later resume to check it.
func TestIncrementalWriter_StampsMetaOnEveryFlush(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ann.yaml")
	require.NoError(t, WriteFile(&File{
		Meta:    meta.Meta{Judge: &meta.Judge{Strategy: "test"}, PoolRef: "old-pool.yaml"},
		Queries: []Entry{{QueryID: "q1", Docs: []GradedDoc{{DocID: uuid.New(), Grade: 2}}}},
	}, path))
	stamp := meta.Meta{Judge: &meta.Judge{Strategy: "test"}, PoolRef: "pool.yaml"}

	w := NewIncrementalWriter(path, stamp)
	_, err := w.LoadPrior()
	require.NoError(t, err)
	require.NoError(t, w.Append(QueryProgress{QueryID: "q2"}, Entry{QueryID: "q2"}))

	jf, err := ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "pool.yaml", jf.Meta.PoolRef)
	assert.Equal(t, stamp.Judge, jf.Meta.Judge)
	assert.Len(t, jf.Queries, 2, "the prior query survives the stamp")
}

func TestIncrementalWriter_StampsMetaOnFirstFlush(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ann.yaml")

	w := NewIncrementalWriter(path, meta.Meta{Judge: &meta.Judge{Strategy: "test"}, PoolRef: "pool.yaml"})
	require.NoError(t, w.Append(QueryProgress{QueryID: "q1"}, Entry{QueryID: "q1"}))

	jf, err := ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "pool.yaml", jf.Meta.PoolRef)
}
