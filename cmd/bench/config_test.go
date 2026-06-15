package main

import (
	"context"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/bench/spec"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubVectorStore struct{}

func (stubVectorStore) QueryVector(context.Context, string) ([]float32, error) { return nil, nil }
func (stubVectorStore) DocVectors(context.Context, []uuid.UUID) (map[uuid.UUID][]float32, error) {
	return nil, nil
}

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
