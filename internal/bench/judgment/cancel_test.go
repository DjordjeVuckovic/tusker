package judgment

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type interruptingStrategy struct {
	fakeStrategy
	interrupt context.CancelFunc
}

func (s *interruptingStrategy) Grade(context.Context, GradingQuery, GradingDoc) (int, error) {
	s.interrupt()
	return 2, nil
}

// A query cut short by an interrupt must not reach the sink: its unfinished
// docs would be written as unjudged and the run would report success.
func TestRunner_InterruptedQueryIsNotSunk(t *testing.T) {
	pf, articles := buildPool(t, 5)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sunk := 0
	r := NewRunner(RunnerConfig{
		Strategy: &interruptingStrategy{fakeStrategy: fakeStrategy{name: "fake"}, interrupt: cancel},
		Reader:   &stubStorageReader{articles: articles},
		Sink: func(QueryProgress, Entry) error {
			sunk++
			return nil
		},
	})
	jf, err := r.Run(ctx, pf)

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, jf)
	assert.Zero(t, sunk)
}
