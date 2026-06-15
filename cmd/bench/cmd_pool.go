package main

import (
	"fmt"
	"strings"

	"github.com/DjordjeVuckovic/tusker/internal/bench/engine"
	"github.com/DjordjeVuckovic/tusker/internal/bench/meta"
	"github.com/DjordjeVuckovic/tusker/internal/bench/pool"
	"github.com/DjordjeVuckovic/tusker/internal/bench/runner"
	"github.com/DjordjeVuckovic/tusker/internal/bench/spec"
	"github.com/DjordjeVuckovic/tusker/internal/bench/suite"
	"github.com/DjordjeVuckovic/tusker/internal/bench/trackctx"
	"github.com/spf13/cobra"
)

type poolFlags struct {
	trackArg string
	specPath string
	output   string
	depth    int
}

func newPoolCmd() *cobra.Command {
	var f poolFlags
	cmd := &cobra.Command{
		Use:   "pool [track]",
		Short: "Run queries through all engines, write a TREC-style pool",
		Long: `Generates a deduplicated pool of candidate docs per query, ready to be
judged. Output goes to tracks/<name>/trec/pool.yaml by default; override
with --output for ad-hoc files.

The pool file carries a meta block (run_id, tool, engines, depth) so later
artifacts can attest which pool they were derived from.`,
		Example: `  bench pool fts_quality
  bench pool fts_quality --depth 50
  bench pool --track tracks/fts_quality --output /tmp/adhoc-pool.yaml`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executePool(cmd, f, args)
		},
	}
	cmd.Flags().StringVar(&f.trackArg, "track", "", "Track name or path")
	cmd.Flags().StringVar(&f.specPath, "spec", "", "Override spec.yaml path")
	cmd.Flags().IntVar(&f.depth, "depth", 0, "Top-K per engine (0 = spec.defaults.pool_depth or 100)")
	cmd.Flags().StringVar(&f.output, "output", "", "Override pool output path")
	return cmd
}

func executePool(cmd *cobra.Command, f poolFlags, args []string) error {
	return forEachTrack(cmd.OutOrStdout(), trackctx.Inputs{
		TrackArg:   trackArg(f.trackArg, args),
		SpecPath:   f.specPath,
		OutputPath: f.output,
	}, func(tr *trackctx.Track) error {
		return poolTrack(cmd, f, tr)
	})
}

func poolTrack(cmd *cobra.Command, f poolFlags, tr *trackctx.Track) error {
	bs, err := spec.LoadFromFile(tr.Spec)
	if err != nil {
		return fmt.Errorf("load spec: %w", err)
	}

	depth := f.depth
	if depth == 0 {
		depth = bs.Defaults.PoolDepth
	}
	if depth == 0 {
		depth = 100
	}

	runCfg := runner.Config{
		KValues:          []int{depth},
		MaxK:             depth,
		Runs:             1,
		QueryParallelism: runner.QueryParallelismUnlimited,
	}

	printSpecWarnings(cmd.OutOrStdout(), bs)

	vectorStore, err := buildQueryVectorStore(cmd.Context(), bs)
	if err != nil {
		return fmt.Errorf("build vector store: %w", err)
	}
	if err := requireEmbedder(bs, vectorStore); err != nil {
		return err
	}
	runCfg.VectorStore = vectorStore

	executors, cleanup, err := createExecutors(cmd.Context(), bs)
	if err != nil {
		return fmt.Errorf("create executors: %w", err)
	}
	defer cleanup()

	r := runner.New(runCfg)
	sp := startSpinner("Pooling " + tr.Name() + "…")
	result, err := r.RunAll(cmd.Context(), bs, executors)
	sp.Stop()
	if err != nil {
		return fmt.Errorf("pool run: %w", err)
	}

	descs, err := collectQueryDescriptions(bs)
	if err != nil {
		return fmt.Errorf("load query descriptions: %w", err)
	}

	pf := buildPoolFile(result, descs, depth)
	pf.Meta = meta.New("pool")
	pf.Meta.SpecID = bs.ID
	pf.Meta.PoolDepth = depth
	pf.Meta.Engines = collectEngines(bs, result)

	outPath := f.output
	if outPath == "" {
		outPath = tr.Pool
	}
	if err := pool.WritePoolFile(pf, outPath); err != nil {
		return fmt.Errorf("write pool: %w", err)
	}
	printDone(cmd.OutOrStdout(), fmt.Sprintf("Pool written: %s  (queries=%d  run_id=%s)", outPath, len(pf.Queries), pf.Meta.RunID))
	return nil
}

// buildPoolFile merges results across all jobs into one entry per query_id.
// Multiple jobs commonly share a suite (e.g. a 3-engine job + an all-engines
// job), so the same query_id surfaces in several JobResults. We union the
// per-engine executions by query_id so each query is pooled — and later judged
// — exactly once, regardless of how many jobs touched it.
func buildPoolFile(result *runner.BenchmarkResult, descs map[string]string, depth int) *pool.PoolFile {
	pf := &pool.PoolFile{}

	var order []string                                       // first-seen query order
	byQuery := make(map[string]map[string]*engine.Execution) // query_id → engine → execution
	jobSeen := make(map[string]struct{})
	var jobNames []string

	for _, jr := range result.Jobs {
		if _, ok := jobSeen[jr.JobName]; !ok {
			jobSeen[jr.JobName] = struct{}{}
			jobNames = append(jobNames, jr.JobName)
		}
		for _, qID := range jr.QueryOrder {
			execs, ok := byQuery[qID]
			if !ok {
				execs = make(map[string]*engine.Execution)
				byQuery[qID] = execs
				order = append(order, qID)
			}
			engResults := jr.Results[qID]
			for _, engName := range jr.EngineNames {
				// First job to contribute an engine wins; the same engine on
				// the same suite/query produces an identical ranked list, so
				// there's nothing to merge — we only need engines not yet seen.
				if _, exists := execs[engName]; exists {
					continue
				}
				qr, ok := engResults[engName]
				if !ok || qr.Error != nil {
					continue
				}
				execs[engName] = &engine.Execution{
					RankedDocIDs: qr.RankedDocIDs,
					TotalMatches: qr.TotalMatches,
				}
			}
		}
	}

	pf.SuiteName = strings.Join(jobNames, ",")
	for _, qID := range order {
		pf.Queries = append(pf.Queries, pool.PoolEntry{
			QueryID:   qID,
			QueryDesc: descs[qID],
			Docs:      pool.PoolResults(byQuery[qID], depth),
		})
	}
	return pf
}

func collectQueryDescriptions(bs *spec.BenchSpec) (map[string]string, error) {
	descs := make(map[string]string)
	seen := make(map[string]struct{})
	for _, job := range bs.Jobs {
		if _, ok := seen[job.Suite]; ok {
			continue
		}
		seen[job.Suite] = struct{}{}
		ls, err := suite.LoadFromFile(job.Suite)
		if err != nil {
			return nil, fmt.Errorf("load suite %q for job %q: %w", job.Suite, job.Name, err)
		}
		for _, q := range ls.Suite.Queries {
			if q.Description != "" {
				descs[q.ID] = q.Description
			}
		}
	}
	return descs, nil
}

func collectEngines(bs *spec.BenchSpec, _ *runner.BenchmarkResult) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, job := range bs.Jobs {
		for _, eng := range job.Engines {
			if _, ok := seen[eng]; ok {
				continue
			}
			seen[eng] = struct{}{}
			out = append(out, eng)
		}
	}
	return out
}
