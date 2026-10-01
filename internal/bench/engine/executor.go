package engine

import (
	"context"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/bench/suite"
	"github.com/google/uuid"
)

type Executor interface {
	Execute(ctx context.Context, query string, args []any) (*Execution, error)
	// Dialect is how a suite's bound {{$name}} values reach this engine.
	Dialect() suite.Dialect
	Name() string
	Close() error
}

// Validator is an optional capability for executors that can syntactically
// validate a query without executing it. PG uses EXPLAIN, ES uses
// _validate/query, API parses the request descriptor. The CLI's `validate`
// subcommand uses this to fail fast on broken queries before a real run.
type Validator interface {
	Validate(ctx context.Context, query string, args []any) error
}

// CorpusCounter is an optional capability for executors that can report it.
type CorpusCounter interface {
	CorpusCount(ctx context.Context) (int64, error)
}

type Execution struct {
	RankedDocIDs  []uuid.UUID
	CorpusMatches *int64 // nil when the engine cannot report one
	Latency       time.Duration
}

var (
	_ Executor  = (*PgExecutor)(nil)
	_ Executor  = (*EsExecutor)(nil)
	_ Executor  = (*APIExecutor)(nil)
	_ Validator = (*PgExecutor)(nil)
	_ Validator = (*EsExecutor)(nil)
	_ Validator = (*APIExecutor)(nil)
)
