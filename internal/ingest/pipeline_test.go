package ingest

import (
	"context"
	"errors"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubCollector struct {
	results []Result[document.Article]
}

func (s *stubCollector) Collect(context.Context) (<-chan Result[document.Article], error) {
	ch := make(chan Result[document.Article], len(s.results))
	for _, r := range s.results {
		ch <- r
	}
	close(ch)
	return ch, nil
}

type recordingIndexer struct {
	saved      []document.Article
	bulkCalls  int
	failBulkOn int // 1-based bulk call that fails; 0 = never
	failSave   bool
}

func (r *recordingIndexer) Save(_ context.Context, a document.Article) (uuid.UUID, error) {
	if r.failSave {
		return uuid.Nil, errors.New("save refused")
	}
	r.saved = append(r.saved, a)
	return uuid.New(), nil
}

func (r *recordingIndexer) SaveBulk(_ context.Context, articles []document.Article) error {
	r.bulkCalls++
	if r.failBulkOn == r.bulkCalls {
		return errors.New("bulk refused")
	}
	r.saved = append(r.saved, articles...)
	return nil
}

func articles(n int) []Result[document.Article] {
	out := make([]Result[document.Article], 0, n)
	for i := 0; i < n; i++ {
		out = append(out, Result[document.Article]{Result: document.Article{Title: "article"}})
	}
	return out
}

func failures(n int) []Result[document.Article] {
	out := make([]Result[document.Article], 0, n)
	for i := 0; i < n; i++ {
		out = append(out, Result[document.Article]{Err: errors.New("unmappable record")})
	}
	return out
}

func TestRun_StoringNothingIsAnError(t *testing.T) {
	tests := []struct {
		name    string
		results []Result[document.Article]
		bulk    bool
		wantMsg string
	}{
		{"every record fails, row by row", failures(3), false, "all 3 records failed"},
		{"every record fails, bulk", failures(3), true, "all 3 records failed"},
		{"source yields nothing, row by row", nil, false, "no records"},
		{"source yields nothing, bulk", nil, true, "no records"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := &recordingIndexer{}
			opts := []PipelineOption{}
			if tt.bulk {
				opts = append(opts, WithBulk(2))
			}
			p := NewPipeline(&stubCollector{results: tt.results}, idx, opts...)

			err := p.Run(context.Background())

			require.Error(t, err, "a load that persisted nothing must not report success")
			assert.Contains(t, err.Error(), "stored 0 articles")
			assert.Contains(t, err.Error(), tt.wantMsg)
			assert.Empty(t, idx.saved)
		})
	}
}

func TestRun_BulkFailureStopsTheLoad(t *testing.T) {
	idx := &recordingIndexer{failBulkOn: 2}
	p := NewPipeline(&stubCollector{results: articles(6)}, idx, WithBulk(2))

	err := p.Run(context.Background())

	require.Error(t, err, "a batch that never landed must fail the run, not just log")
	assert.Contains(t, err.Error(), "save 2 articles")
	assert.Equal(t, 2, idx.bulkCalls,
		"the run must abort at the failed batch rather than write the rest over a corpus known to be incomplete")
}

func TestRun_TrailingPartialBatchIsStored(t *testing.T) {
	idx := &recordingIndexer{}
	p := NewPipeline(&stubCollector{results: articles(7)}, idx, WithBulk(5))

	require.NoError(t, p.Run(context.Background()))
	assert.Len(t, idx.saved, 7, "the articles left over after the last full batch must still be stored")
}

func TestRun_PartialFailuresStillSucceed(t *testing.T) {
	idx := &recordingIndexer{}
	mixed := append(articles(2), failures(1)...)
	p := NewPipeline(&stubCollector{results: mixed}, idx, WithBulk(2))

	require.NoError(t, p.Run(context.Background()),
		"a run that stored something is a success even with skipped records")
	assert.Len(t, idx.saved, 2)
}
