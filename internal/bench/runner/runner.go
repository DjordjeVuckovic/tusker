package runner

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/bench/engine"
	"github.com/DjordjeVuckovic/tusker/internal/bench/metrics"
	"github.com/DjordjeVuckovic/tusker/internal/bench/spec"
	"github.com/DjordjeVuckovic/tusker/internal/bench/suite"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
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
	if err := VerifyEmbeddingModel(bs.Engines, r.config.VectorStore); err != nil {
		return nil, err
	}

	br := &BenchmarkResult{Config: r.config}

	bindings := queryBindings(bs)

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

		jr, err := r.RunJob(ctx, JobRequest{
			Job:       job,
			Suite:     loaded,
			Executors: executors,
			Bindings:  bindings,
		})
		if err != nil {
			return nil, fmt.Errorf("run job %q: %w", job.Name, err)
		}
		br.Jobs = append(br.Jobs, jr)
	}

	return br, nil
}

// VerifyEmbeddingModel rejects an engine that declares a different embedding
// model from the one the store embeds queries with. A vector template filters
// document vectors by the declared model while the query is embedded by the
// store, so a disagreement means every cosine distance is computed across two
// vector spaces — arithmetic that succeeds and ranks nothing. Engines that
// declare no model, and tracks with no store at all, are left alone.
func VerifyEmbeddingModel(engines map[string]spec.Engine, store storage.VectorStore) error {
	if store == nil {
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(engines)) {
		declared, _ := engines[name].Params[suite.EmbeddingModelParam].(string)
		if declared == "" || declared == store.Model() {
			continue
		}
		return fmt.Errorf(
			"engine %q declares %s %q but queries are embedded with %q: set EMBEDDING_MODEL to the declared model or fix the spec",
			name, suite.EmbeddingModelParam, declared, store.Model())
	}
	return nil
}

// queryBindings resolves every engine's query block and declared params once,
// so the per-query fan-out never reaches back into the spec.
func queryBindings(bs *spec.BenchSpec) map[string]spec.QueryBinding {
	bindings := make(map[string]spec.QueryBinding, len(bs.Engines))
	for name := range bs.Engines {
		bindings[name] = bs.QueryBinding(name)
	}
	return bindings
}

// JobRequest is everything one job needs to run: its declaration, the loaded
// suite, the executors to drive, and how each engine reaches its queries.
type JobRequest struct {
	Job       spec.Job
	Suite     *suite.LoadedSuite
	Executors map[string]engine.Executor
	Bindings  map[string]spec.QueryBinding
}

func (r *Runner) RunJob(ctx context.Context, req JobRequest) (*JobResult, error) {
	jobExecutors := make(map[string]engine.Executor)
	for _, engName := range req.Job.Engines {
		exec, ok := req.Executors[engName]
		if !ok {
			return nil, fmt.Errorf("executor %q not found", engName)
		}
		jobExecutors[engName] = exec
	}

	jr := &JobResult{
		JobName:     req.Job.Name,
		Results:     make(map[string]map[string]QueryResult),
		EngineNames: req.Job.Engines,
	}

	jobReq := req
	jobReq.Executors = jobExecutors
	r.runQueries(ctx, jr, jobReq)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return jr, nil
}

func (r *Runner) runQueries(ctx context.Context, jr *JobResult, req JobRequest) {
	queries := req.Suite.Suite.Queries
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
			if ctx.Err() != nil {
				return
			}
			r.runEnginesForQuery(ctx, jr, q, req, engineSem)
		}()
	}
	wg.Wait()
}

// runEnginesForQuery fans out to all engines for a single query concurrently.
// Each goroutine writes only to its own index in the slots slice (no mutex),
// and the merge into jr.Results happens after all goroutines finish.
func (r *Runner) runEnginesForQuery(ctx context.Context, jr *JobResult, q *suite.Query, req JobRequest, engineSem chan struct{}) {
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
		exec, ok := req.Executors[engName]
		if !ok {
			continue
		}
		idx, engName := idx, engName
		wg.Add(1)
		go func() {
			defer wg.Done()
			engineSem <- struct{}{}
			defer func() { <-engineSem }()

			// An engine with no declared binding reads its own block; an empty
			// QuerySource would instead match nothing and drop the query silently.
			binding, declared := req.Bindings[engName]
			if !declared {
				binding = spec.QueryBinding{QuerySource: engName}
			}
			resolved, err := q.ResolveEngineQuery(suite.ResolveOptions{
				Engine:   binding.QuerySource,
				Registry: req.Suite.Registry,
				SuiteDir: req.Suite.Dir,
				Defaults: binding.Params,
				Extra:    extra,
			})
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
					QueryID:       q.ID,
					Category:      q.Category,
					EngineName:    engName,
					Scores:        scores,
					RankedDocIDs:  result.rankedIDs,
					ReturnedCount: len(result.rankedIDs),
					CorpusMatches: result.corpusMatches,
					Latency:       result.latencyStats,
					Error:         result.err,
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
	rankedIDs     []uuid.UUID
	corpusMatches *int64
	latencyStats  LatencyStats
	err           error
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
		rankedIDs:     lastExec.RankedDocIDs,
		corpusMatches: lastExec.CorpusMatches,
		latencyStats:  ComputeLatencyStats(latencies),
	}
}
