package storage

import (
	"context"
)

type ExecOptions struct {
	TimeoutSeconds int
}

type ExecuteResult struct {
	RowCount int
	Hits     []map[string]interface{}
}

// RawExecutor defines the interface for executing db queries.
type RawExecutor interface {
	// Exec executes a query with the given parameters and options
	// Order of params must match the order of placeholders in the query.
	Exec(ctx context.Context, query string, params []interface{}, baseOpts *ExecOptions) (*ExecuteResult, error)
}
