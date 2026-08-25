package main

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/embedding"
	"github.com/DjordjeVuckovic/tusker/internal/embedding/embedfile"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sliceReader struct {
	records []embedfile.Record
	pos     int
}

func (s *sliceReader) Read(out []embedfile.Record) (int, error) {
	if s.pos >= len(s.records) {
		return 0, io.EOF
	}
	n := copy(out, s.records[s.pos:])
	s.pos += n
	return n, nil
}

// stubIndexer stores everything it is given unless orphans says otherwise.
type stubIndexer struct {
	orphansPerBatch int
	err             error
	batches         int
}

func (s *stubIndexer) Save(context.Context, *embedding.Vec) (uuid.UUID, error) {
	return uuid.Nil, errors.New("unused")
}

func (s *stubIndexer) SaveBulk(_ context.Context, vecs []*embedding.Vec) (storage.EmbedWriteResult, error) {
	s.batches++
	if s.err != nil {
		return storage.EmbedWriteResult{}, s.err
	}
	skipped := min(s.orphansPerBatch, len(vecs))
	return storage.EmbedWriteResult{Stored: len(vecs) - skipped, Skipped: skipped}, nil
}

func records(n int, dim int) []embedfile.Record {
	out := make([]embedfile.Record, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, embedfile.Record{ID: uuid.NewString(), Embedding: make([]float32, dim)})
	}
	return out
}

func TestEmbedIngest_CountsWhatTheIndexerConfirmed(t *testing.T) {
	idx := &stubIndexer{orphansPerBatch: 1}
	ingest := embedIngest{
		Reader:    &sliceReader{records: records(7, expectedDim)},
		Indexer:   idx,
		Model:     "qwen3-embedding:0.6b",
		BatchSize: 3,
	}

	stats, err := ingest.run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 7, stats.Sent, "every valid record reaches the indexer")
	assert.Equal(t, 4, stats.Stored, "one orphan per batch across three batches")
	assert.Equal(t, 3, stats.Skipped)
	assert.Equal(t, 3, idx.batches, "the trailing partial batch must still be flushed")
}

func TestEmbedIngest_RejectsMalformedRecordsWithoutSendingThem(t *testing.T) {
	bad := []embedfile.Record{
		{ID: "not-a-uuid", Embedding: make([]float32, expectedDim)},
		{ID: uuid.NewString(), Embedding: make([]float32, expectedDim-1)},
		{ID: uuid.NewString(), Embedding: make([]float32, expectedDim)},
	}
	idx := &stubIndexer{}
	ingest := embedIngest{Reader: &sliceReader{records: bad}, Indexer: idx, BatchSize: 10}

	stats, err := ingest.run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, stats.BadIDs)
	assert.Equal(t, 1, stats.BadDim)
	assert.Equal(t, 1, stats.Sent, "only the well-formed record is sent")
	assert.Equal(t, 1, stats.Stored)
}

func TestEmbedIngest_PropagatesIndexerFailure(t *testing.T) {
	ingest := embedIngest{
		Reader:    &sliceReader{records: records(2, expectedDim)},
		Indexer:   &stubIndexer{err: errors.New("elasticsearch rejected the bulk request")},
		BatchSize: 10,
	}

	_, err := ingest.run(context.Background())

	require.Error(t, err, "a write that never landed must not be reported as a skip")
	assert.Contains(t, err.Error(), "rejected")
}

func TestEmbedIngest_EveryArticleMissingStoresNothing(t *testing.T) {
	ingest := embedIngest{
		Reader:    &sliceReader{records: records(5, expectedDim)},
		Indexer:   &stubIndexer{orphansPerBatch: 5},
		BatchSize: 5,
	}

	stats, err := ingest.run(context.Background())

	require.NoError(t, err, "the indexer succeeded; it is runEmbeddings that must reject this")
	assert.Zero(t, stats.Stored, "a load against a database with no articles stores nothing")
	assert.Equal(t, 5, stats.Skipped)
}
