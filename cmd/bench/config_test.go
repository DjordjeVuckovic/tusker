package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/bench/spec"
	"github.com/DjordjeVuckovic/tusker/internal/bench/trackctx"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubVectorStore struct{ model string }

func (stubVectorStore) QueryVector(context.Context, string) ([]float32, error) { return nil, nil }
func (stubVectorStore) DocVectors(context.Context, []uuid.UUID) (map[uuid.UUID][]float32, error) {
	return nil, nil
}
func (s stubVectorStore) Model() string { return s.model }

func TestRequireEmbedder(t *testing.T) {
	t.Run("semantic without store fails", func(t *testing.T) {
		err := requireEmbedder(&spec.BenchSpec{Kind: spec.KindSemantic}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "EMBEDDING_BASE_URL")
	})

	t.Run("semantic with store ok", func(t *testing.T) {
		var store storage.VectorStore = stubVectorStore{}
		require.NoError(t, requireEmbedder(&spec.BenchSpec{Kind: spec.KindSemantic}, store))
	})

	t.Run("non-vector kind without store ok", func(t *testing.T) {
		require.NoError(t, requireEmbedder(&spec.BenchSpec{Kind: spec.KindFTS}, nil))
	})

	t.Run("empty kind without store ok", func(t *testing.T) {
		require.NoError(t, requireEmbedder(&spec.BenchSpec{}, nil))
	})
}

// An interrupt ends a glob run at the track it cut short; the remaining tracks
// would otherwise run against a cancelled context and all report failure.
func TestForEachTrack_InterruptStopsGlob(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		dir := filepath.Join(root, "g", name)
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "trec"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.yaml"), nil, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "suite.yaml"), nil, 0o644))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var ran []string
	err := forEachTrack(ctx, io.Discard, trackctx.Inputs{TrackArg: "g/*", TrackRoot: root}, func(tr *trackctx.Track) error {
		ran = append(ran, tr.Name())
		cancel()
		return errors.New("30 query/engine pair(s) failed validation")
	})

	require.Error(t, err)
	assert.Len(t, ran, 1)
}
