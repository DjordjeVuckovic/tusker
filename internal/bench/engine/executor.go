package engine

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Request is one suite query as an engine receives it.
type Request struct {
	// Query is SQL, a Query DSL body, or an API request descriptor.
	Query string
	// Args are the values behind a Postgres statement's $1 … $n, in order.
	Args []any
}

type Executor interface {
	Execute(ctx context.Context, req Request) (*Execution, error)
	Name() string
	Close() error
}

// Validator is an optional capability for executors that can syntactically
// validate a query without executing it. PG uses EXPLAIN, ES uses
// _validate/query, API parses the request descriptor. The CLI's `validate`
// subcommand uses this to fail fast on broken queries before a real run.
type Validator interface {
	Validate(ctx context.Context, req Request) error
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
