package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/storage/pg"
	"github.com/google/uuid"
)

type PgExecutor struct {
	name     string
	executor storage.RawExecutor
}

func NewPgExecutor(name string, pool *pg.ConnectionPool) *PgExecutor {
	return &PgExecutor{
		name:     name,
		executor: pg.NewRawExecutor(pool),
	}
}

func (e *PgExecutor) Execute(ctx context.Context, rawQuery string, params []any) (*Execution, error) {
	start := time.Now()

	result, err := e.executor.Exec(ctx, rawQuery, params, nil)
	if err != nil {
		return nil, fmt.Errorf("pg exec: %w", err)
	}

	latency := time.Since(start)

	ids := make([]uuid.UUID, 0, len(result.Hits))
	for _, hit := range result.Hits {
		id, err := extractUUID(hit["id"])
		if err != nil {
			return nil, fmt.Errorf("pg extract id: %w", err)
		}
		ids = append(ids, id)
	}

	return &Execution{
		RankedDocIDs: ids,
		TotalMatches: int64(result.TotalHits),
		Latency:      latency,
	}, nil
}

func (e *PgExecutor) Name() string { return e.name }
func (e *PgExecutor) Close() error { return nil }

// Validate runs EXPLAIN on the query. This catches syntax errors and missing
// columns/tables/operators without scanning data. ParadeDB's pdb.* functions
// also surface here, so it's a real correctness check for those queries too.
func (e *PgExecutor) Validate(ctx context.Context, query string) error {
	if _, err := e.executor.Exec(ctx, "EXPLAIN "+query, nil, nil); err != nil {
		return fmt.Errorf("pg explain: %w", err)
	}
	return nil
}

func extractUUID(val interface{}) (uuid.UUID, error) {
	switch v := val.(type) {
	case [16]byte:
		return uuid.UUID(v), nil
	case uuid.UUID:
		return v, nil
	case string:
		return uuid.Parse(v)
	default:
		return uuid.Nil, fmt.Errorf("unsupported id type %T", val)
	}
}
