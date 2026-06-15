package runner

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/bench/engine"
	"github.com/DjordjeVuckovic/tusker/internal/bench/metrics"
	"github.com/DjordjeVuckovic/tusker/internal/bench/spec"
	"github.com/DjordjeVuckovic/tusker/internal/bench/suite"
	"github.com/google/uuid"
)

type Runner struct {
	config Config
}

func New(cfg Config) *Runner {
	return &Runner{config: cfg}
}

func (r *Runner) RunAll(
	ctx context.Context,
	bs *spec.BenchSpec,
	executors map[string]engine.Executor,
) (*BenchmarkResult, error) {
	br := &BenchmarkResult{Config: r.config}

	// Cache suite loads — multiple jobs commonly share a suite.
	suiteCache := map[string]*suite.LoadedSuite{}
	for _, job := range bs.Jobs {
		loaded, ok := suiteCache[job.Suite]
		if !ok {
			ls, err := suite.LoadFromFile(job.Suite)
			if err != nil {
				return nil, fmt.Errorf("load suite for job %q: %w", job.Name, err)
			}
			suiteCache[job.Suite] = ls
			loaded = ls
		}

		jr, err := r.RunJob(ctx, job, loaded, executors)
		if err != nil {
			return nil, fmt.Errorf("run job %q: %w", job.Name, err)
		}
		br.Jobs = append(br.Jobs, jr)
	}

	return br, nil
}

func (r *Runner) RunJob(
	ctx context.Context,
	job spec.Job,
	loaded *suite.LoadedSuite,
	executors map[string]engine.Executor,
) (*JobResult, error) {
	jobExecutors := make(map[string]engine.Executor)
	for _, engName := range job.Engines {
		exec, ok := executors[engName]
		if !ok {
			return nil, fmt.Errorf("executor %q not found", engName)
		}
		jobExecutors[engName] = exec
	}

	jr := &JobResult{
		JobName:     job.Name,
		Results:     make(map[string]map[string]QueryResult),
		EngineNames: job.Engines,
	}

	r.runQueries(ctx, jr, loaded.Suite.Queries, loaded.Registry, jobExecutors, loaded.Dir)

	return jr, nil
}

func (r *Runner) runQueries(
	ctx context.Context,
	jr *JobResult,
	queries []suite.Query,
	registry *suite.TemplateRegistry,
	executors map[string]engine.Executor,
	suiteDir string,
) {
	// Pre-populate order and result maps sequentially before launching any
	// goroutines. Goroutines only READ the outer jr.Results map (to get their
	// inner map pointer) and write only to their own inner map — no races.
	for i := range queries {
		jr.QueryOrder = append(jr.QueryOrder, queries[i].ID)
		jr.Results[queries[i].ID] = make(map[string]QueryResult)
	}

	// querySem controls how many queries execute concurrently.
	// QueryParallelismSerial (1) = one at a time → clean latency numbers.
	// QueryParallelismUnlimited (0) → all queries in parallel → faster pool/validate.
	qp := r.config.QueryParallelism
	if qp <= 0 {
		qp = len(queries)
	}
	querySem := make(chan struct{}, qp)

	// engineSem is shared across all query goroutines — it bounds total
	// concurrent engine calls globally, not just per query.
	ep := r.config.EngineParallelism
	if ep <= 0 {
		ep = len(jr.EngineNames)
	}
	engineSem := make(chan struct{}, ep)

	var wg sync.WaitGroup
	for i := range queries {
		q := &queries[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			querySem <- struct{}{}
			defer func() { <-querySem }()
			r.runEnginesForQuery(ctx, jr, q, registry, executors, suiteDir, engineSem)
		}()
	}
	wg.Wait()
}

// runEnginesForQuery fans out to all engines for a single query concurrently.
// Each goroutine writes only to its own index in the slots slice (no mutex),
// and the merge into jr.Results happens after all goroutines finish.
func (r *Runner) runEnginesForQuery(
	ctx context.Context,
	jr *JobResult,
	q *suite.Query,
	registry *suite.TemplateRegistry,
	executors map[string]engine.Executor,
	suiteDir string,
	engineSem chan struct{},
) {
	judgments := r.judgmentsFor(q)
	extra := r.queryVectorParams(ctx, q)

	type slot struct {
		engName string
		qr      QueryResult
		present bool
	}
	slots := make([]slot, len(jr.EngineNames))
	for idx, name := range jr.EngineNames {
		slots[idx].engName = name
	}

	var wg sync.WaitGroup
	for idx, engName := range jr.EngineNames {
		exec, ok := executors[engName]
		if !ok {
			continue
		}
		idx, engName := idx, engName
		wg.Add(1)
		go func() {
			defer wg.Done()
			engineSem <- struct{}{}
			defer func() { <-engineSem }()

			resolved, err := q.ResolveEngineQuery(engName, registry, suiteDir, extra)
			if err != nil {
				slots[idx] = slot{
					engName: engName,
					qr:      QueryResult{QueryID: q.ID, EngineName: engName, Error: fmt.Errorf("resolve query: %w", err)},
					present: true,
				}
				slog.Warn("resolve query failed", "query", q.ID, "engine", engName, "error", err)
				return
			}
			if resolved == nil {
				return
			}

			result := r.executeWithRetries(ctx, exec, resolved.Query, nil, r.config.WarmupRuns, r.config.Runs)

			var scores metrics.ScoreSet
			if result.err == nil && len(judgments) > 0 {
				scores = metrics.ComputeAll(result.rankedIDs, judgments, r.config.KValues, r.config.RelevanceThreshold)
			}
			if result.err != nil {
				slog.Warn("query failed", "query", q.ID, "engine", engName, "error", result.err)
			}

			slots[idx] = slot{
				engName: engName,
				qr: QueryResult{
					QueryID:      q.ID,
					EngineName:   engName,
					Scores:       scores,
					RankedDocIDs: result.rankedIDs,
					TotalMatches: result.totalMatches,
					Latency:      result.latencyStats,
					Error:        result.err,
				},
				present: true,
			}
		}()
	}
	wg.Wait()

	for _, s := range slots {
		if s.present {
			jr.Results[q.ID][s.engName] = s.qr
		}
	}
}

// queryVectorParams embeds the query once (via the configured VectorStore) and
// returns it under the reserved query-vector param, so resolution can inject it
// into vector queries. Returns nil when there is no store or the query needs no
// vector; an embedding failure is logged and the vector queries simply fail to
// resolve (recorded per-engine, non-fatal).
func (r *Runner) queryVectorParams(ctx context.Context, q *suite.Query) suite.TemplateParams {
	if r.config.VectorStore == nil || !q.NeedsQueryVector() {
		return nil
	}
	vec, err := r.config.VectorStore.QueryVector(ctx, q.Description)
	if err != nil {
		slog.Warn("query embedding failed; vector queries for this query will not resolve",
			"query", q.ID, "error", err)
		return nil
	}
	return suite.TemplateParams{suite.ReservedQueryVectorParam: suite.FormatVector(vec)}
}

// judgmentsFor returns the relevance grades for a query. Priority: the
// runner-level Config.Judgments map (loaded by the CLI from the resolved
// annotations file) takes precedence over any judgments embedded in the suite
// (which is the case only when a suite is hand-edited — rare in v1).
func (r *Runner) judgmentsFor(q *suite.Query) map[uuid.UUID]int {
	if r.config.Judgments != nil {
		if perQuery, ok := r.config.Judgments[q.ID]; ok {
			out := make(map[uuid.UUID]int, len(perQuery))
			for idStr, grade := range perQuery {
				if id, err := uuid.Parse(idStr); err == nil {
					out[id] = grade
				}
			}
			return out
		}
	}
	return q.JudgmentMap()
}

type execResult struct {
	rankedIDs    []uuid.UUID
	totalMatches int64
	latencyStats LatencyStats
	err          error
}

func (r *Runner) executeWithRetries(
	ctx context.Context,
	exec engine.Executor,
	query string,
	params []any,
	warmup, runs int,
) execResult {
	for i := 0; i < warmup; i++ {
		_, _ = exec.Execute(ctx, query, params)
	}

	var latencies []time.Duration
	var lastExec *engine.Execution
	var lastErr error

	for i := 0; i < runs; i++ {
		result, err := exec.Execute(ctx, query, params)
		if err != nil {
			lastErr = err
			continue
		}
		lastExec = result
		latencies = append(latencies, result.Latency)
	}

	if lastExec == nil {
		return execResult{err: lastErr}
	}

	return execResult{
		rankedIDs:    lastExec.RankedDocIDs,
		totalMatches: lastExec.TotalMatches,
		latencyStats: ComputeLatencyStats(latencies),
	}
}
