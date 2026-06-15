package runner

import "github.com/DjordjeVuckovic/tusker/internal/storage"

var DefaultKValues = []int{3, 5, 10}

const (
	DefaultMaxK               = 10
	DefaultRelevanceThreshold = 1
	DefaultWarmupRuns         = 1
	DefaultRuns               = 3
	DefaultEngineParallelism  = 4

	// QueryParallelismSerial runs one query at a time. Use for bench run so
	// each query gets the engine's undivided attention and latency numbers are
	// clean (no cross-query resource contention).
	QueryParallelismSerial = 1

	// QueryParallelismUnlimited fans out all queries concurrently. Use for
	// bench pool and bench validate where latency measurement doesn't matter
	// and throughput does.
	QueryParallelismUnlimited = 0
)

type Config struct {
	KValues            []int
	MaxK               int
	RelevanceThreshold int
	WarmupRuns         int
	Runs               int
	// QueryParallelism bounds concurrent queries. QueryParallelismSerial (1)
	// = one at a time; QueryParallelismUnlimited (0) = all at once.
	QueryParallelism int
	// EngineParallelism bounds concurrent engine calls within a single query.
	// 0 = unlimited (fan out to all engines simultaneously).
	EngineParallelism int
	// Judgments[queryID][docID]grade — pre-loaded by the CLI from the
	// resolved annotations file. When nil, queries are scored without
	// relevance grades and the report flags them as unjudged.
	Judgments map[string]map[string]int
	// VectorStore, when set, embeds queries that reference the reserved
	// query-vector placeholder and injects the result before execution. nil for
	// non-vector tracks (and validate, which is structural only).
	VectorStore storage.VectorStore
}

func DefaultConfig() Config {
	return Config{
		KValues:            DefaultKValues,
		MaxK:               DefaultMaxK,
		RelevanceThreshold: DefaultRelevanceThreshold,
		WarmupRuns:         DefaultWarmupRuns,
		Runs:               DefaultRuns,
		QueryParallelism:   QueryParallelismSerial,
		EngineParallelism:  DefaultEngineParallelism,
	}
}
