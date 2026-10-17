package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/bench/engine"
	"github.com/DjordjeVuckovic/tusker/internal/bench/judgment"
	"github.com/DjordjeVuckovic/tusker/internal/bench/meta"
	"github.com/DjordjeVuckovic/tusker/internal/bench/pool"
	"github.com/DjordjeVuckovic/tusker/internal/bench/report"
	"github.com/DjordjeVuckovic/tusker/internal/bench/runner"
	"github.com/DjordjeVuckovic/tusker/internal/bench/spec"
	"github.com/DjordjeVuckovic/tusker/internal/bench/trackctx"
	"github.com/spf13/cobra"
)

type runFlags struct {
	trackArg    string
	specPath    string
	suitePath   string
	judgments   string
	output      string
	kValues     string
	jobs        string // comma-separated job name filter
	warmup      int
	iters       int
	latencyOnly bool
}

func newRunCmd() *cobra.Command {
	var f runFlags
	cmd := &cobra.Command{
		Use:   "run [track]",
		Short: "Execute the benchmark + produce a report",
		Long: `Runs every job in the track's spec.yaml against its engines, computes IR
metrics + latency, writes a JSON report under <track>/reports/.

The track arg is a path — absolute, or relative to --track-root (default: cwd) —
or a glob, which runs every matching track and writes one report per track.

The judgments file used for scoring resolves in this order:
  1. --judgments <name|path>      (CLI override)
  2. spec.defaults.judgments      (per-track default; a relative path starts at the track)
  3. none → the run fails before measuring anything

A missing judgments file fails the run the same way. --latency-only measures
latency without judgments; its report is marked latency_only and never becomes
latest.json.

The report is always written, but bench run exits non-zero and leaves
latest.json alone when any (query, engine) execution errored or is one the
pool dropped (--allow-partial), or the judgments hold pooled docs left unjudged. latest.json also stays put unless the run took
every spec job at the spec's warmup, iterations and K.`,
		Example: `  bench run tracks/global-news-dataset/fts_quality
  bench run tracks/global-news-dataset/news_semantic
  bench run 'tracks/global-news-dataset/*'        # every paradigm of the dataset
  bench run tracks/global-news-dataset/fts_quality --judgments claude-api`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeRun(cmd, f, args)
		},
	}
	cmd.Flags().StringVar(&f.trackArg, "track", "", "Track path (relative to --track-root)")
	cmd.Flags().StringVar(&f.specPath, "spec", "", "Override spec.yaml path")
	cmd.Flags().StringVar(&f.suitePath, "suite", "", "Override suite.yaml path")
	cmd.Flags().StringVar(&f.judgments, "judgments", "", "Strategy name (e.g. lexical) or annotations YAML path")
	cmd.Flags().StringVar(&f.output, "output", "", "Override report path (default: <track>/reports/<run_id>.json)")
	cmd.Flags().StringVar(&f.kValues, "k", "3,5,10", "K cut-offs for NDCG/P/R/F1")
	cmd.Flags().StringVar(&f.jobs, "jobs", "", "Comma-separated job names to run (default: all jobs in spec)")
	cmd.Flags().IntVar(&f.warmup, "warmup", 0, "Warmup iterations")
	cmd.Flags().IntVar(&f.iters, "iterations", 0, "Measured iterations (0 = spec.runs.iterations)")
	cmd.Flags().BoolVar(&f.latencyOnly, "latency-only", false, "Measure latency without judgments; the report never becomes latest.json")
	return cmd
}

func executeRun(cmd *cobra.Command, f runFlags, args []string) error {
	ks, err := parseKList(f.kValues)
	if err != nil {
		return err
	}
	in := trackInputs(f.trackArg, args)
	in.SpecPath = f.specPath
	in.SuitePath = f.suitePath
	in.OutputPath = f.output
	return forEachTrack(cmd.Context(), cmd.OutOrStdout(), in, func(tr *trackctx.Track) error {
		return runTrack(cmd, f, ks, tr)
	})
}

// newRunConfig resolves the measurement knobs for a run: an explicit flag beats
// the spec default, and both parallelism axes stay serial.
func newRunConfig(f runFlags, bs *spec.BenchSpec, ks []int, kFromFlag bool) runner.Config {
	cfg := runner.Config{
		KValues:            ks,
		RelevanceThreshold: bs.Metrics.RelevanceThreshold,
		WarmupRuns:         firstNonZero(f.warmup, bs.Runs.Warmup),
		Runs:               firstNonZero(f.iters, bs.Runs.Iterations),
		QueryParallelism:   runner.QueryParallelismSerial,
		EngineParallelism:  runner.EngineParallelismSerial,
	}
	if len(bs.Metrics.KValues) > 0 && !kFromFlag {
		cfg.KValues = bs.Metrics.KValues
	}
	return cfg
}

func runTrack(cmd *cobra.Command, f runFlags, ks []int, tr *trackctx.Track) error {
	bs, err := spec.LoadFromFile(tr.Spec)
	if err != nil {
		return fmt.Errorf("load spec: %w", err)
	}

	judgments, err := loadRunJudgments(f, bs, tr)
	if err != nil {
		return err
	}

	runCfg := newRunConfig(f, bs, ks, cmd.Flags().Changed("k"))
	runCfg.Judgments = judgments.grades

	printSpecWarnings(cmd.OutOrStdout(), bs)

	vectorStore, err := buildQueryVectorStore(cmd.Context(), bs)
	if err != nil {
		return fmt.Errorf("build vector store: %w", err)
	}
	if err := requireEmbedder(bs, vectorStore); err != nil {
		return err
	}
	runCfg.VectorStore = vectorStore

	// Apply --jobs filter: keep only the named jobs.
	specJobCount := len(bs.Jobs)
	var jobFilter []string
	if f.jobs != "" {
		wanted := make(map[string]bool)
		for _, name := range strings.Split(f.jobs, ",") {
			if n := strings.TrimSpace(name); n != "" {
				wanted[n] = true
			}
		}
		jobFilter = slices.Sorted(maps.Keys(wanted))
		filtered := bs.Jobs[:0]
		for _, job := range bs.Jobs {
			if wanted[job.Name] {
				filtered = append(filtered, job)
			}
		}
		if len(filtered) == 0 {
			var names []string
			for n := range wanted {
				names = append(names, n)
			}
			return fmt.Errorf("--jobs filter matched no jobs (wanted: %s)", strings.Join(names, ", "))
		}
		bs.Jobs = filtered
	}

	executors, cleanup, err := createExecutors(cmd.Context(), bs)
	if err != nil {
		return fmt.Errorf("create executors: %w", err)
	}
	defer cleanup()

	corpus, err := collectCorpusInfo(cmd.Context(), executors)
	if err != nil {
		return err
	}
	indexes := collectIndexProvenance(cmd.Context(), cmd.OutOrStdout(), executors)

	r := runner.New(runCfg)
	sp := startSpinner("Running " + tr.Name() + "…")
	start := time.Now()
	result, err := r.RunAll(cmd.Context(), bs, executors)
	elapsed := time.Since(start)
	sp.Stop()
	if err != nil {
		return fmt.Errorf("run benchmark: %w", err)
	}

	rpt := report.Generate(result, &report.GenerateOptions{
		Spec:    bs,
		Corpus:  corpus,
		Indexes: indexes,
	})
	rpt.Provenance.SpecID = bs.ID
	rpt.Provenance.Sources = &meta.Sources{
		Spec:      tr.Spec,
		Suite:     tr.Suite,
		Pool:      tr.Pool,
		Judgments: judgments.path,
	}
	if judgments.meta != nil {
		rpt.Provenance.Judge = judgments.meta.Judge
		rpt.Provenance.GradedCount = judgments.meta.GradedCount
		rpt.Provenance.PoolRef = judgments.meta.PoolRef
	}
	rpt.Settings = report.RunSettings{
		Warmup:       runCfg.WarmupRuns,
		Iterations:   runCfg.Runs,
		JobFilter:    jobFilter,
		LatencyOnly:  f.latencyOnly,
		EngineParams: declaredEngineParams(bs),
	}
	failures := runFailures(result, judgments)
	rpt.Settings.Incomplete = append(slices.Clone(failures),
		specDeviations(f, len(bs.Jobs) == specJobCount, measuredAsSpecified(runCfg, bs, defaultKValues(cmd)))...)

	report.WriteTable(rpt, os.Stdout)
	fmt.Fprintf(os.Stdout, "%s %s\n", cDim.Sprint("Elapsed:"), elapsed.Round(time.Millisecond))

	outPath := f.output
	if outPath == "" {
		outPath = tr.ReportPath(rpt.Provenance.RunID)
	}
	if err := report.WriteJSON(rpt, outPath); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	printDone(cmd.OutOrStdout(), "Report written: "+outPath)

	if len(failures) > 0 {
		return fmt.Errorf("%s\n  • report written to %s; latest.json was not moved", strings.Join(failures, "\n"), outPath)
	}
	switch {
	case len(rpt.Settings.Incomplete) > 0:
		printWarn(cmd.OutOrStdout(), "latest.json not moved: "+strings.Join(rpt.Settings.Incomplete, "; "))
	case f.output != "":
	default:
		if err := updateLatestPointer(tr.LatestReportPath(), outPath); err != nil {
			printWarn(cmd.OutOrStdout(), fmt.Sprintf("could not update latest.json pointer: %v", err))
		}
	}
	return nil
}

// specDeviations lists how a run departs from the one its spec defines.
func specDeviations(f runFlags, ranEveryJob, asSpecified bool) []string {
	var deviations []string
	if f.latencyOnly {
		deviations = append(deviations, "latency-only run, scored against no judgments")
	}
	if !ranEveryJob {
		deviations = append(deviations, "--jobs left out some of the spec's jobs")
	}
	if !asSpecified {
		deviations = append(deviations, "--warmup, --iterations or --k differ from spec.yaml")
	}
	return deviations
}

// measuredAsSpecified reports whether cfg measures with the spec's own warmup,
// iterations and K, however they were spelled on the command line.
func measuredAsSpecified(cfg runner.Config, bs *spec.BenchSpec, defaultKs []int) bool {
	specCfg := newRunConfig(runFlags{}, bs, defaultKs, false)
	return cfg.WarmupRuns == specCfg.WarmupRuns &&
		cfg.Runs == specCfg.Runs &&
		slices.Equal(cfg.KValues, specCfg.KValues)
}

// defaultKValues is the K a spec without metrics.k_values is measured at.
func defaultKValues(cmd *cobra.Command) []int {
	ks, _ := parseKList(cmd.Flags().Lookup("k").DefValue)
	return ks
}

func declaredEngineParams(bs *spec.BenchSpec) map[string]map[string]any {
	params := make(map[string]map[string]any)
	for name, eng := range bs.Engines {
		if len(eng.Params) > 0 {
			params[name] = eng.Params
		}
	}
	return params
}

// runJudgments is what a run scores against. A latency-only run has none.
type runJudgments struct {
	path     string
	grades   map[string]map[string]int
	meta     *meta.Meta
	coverage judgmentCoverage
	// dropped are the cells the pool was written without (--allow-partial).
	dropped []meta.DroppedExecution
}

// loadRunJudgments resolves --judgments, else spec.defaults.judgments, and
// refuses a run with neither unless it is latency-only, so an unscored report
// never stands in for the track's results.
func loadRunJudgments(f runFlags, bs *spec.BenchSpec, tr *trackctx.Track) (*runJudgments, error) {
	if f.latencyOnly {
		if f.judgments != "" {
			return nil, errors.New("--latency-only scores nothing, so --judgments would be ignored; pass one of them")
		}
		return &runJudgments{}, nil
	}

	explicit := f.judgments != ""
	path := tr.JudgmentsPath(f.judgments)
	if !explicit {
		path = tr.DefaultJudgmentsPath(bs.Defaults.Judgments)
	}
	if path == "" {
		return nil, errors.New("no judgments to score against: set spec.defaults.judgments or pass --judgments\n" +
			"  • or pass --latency-only to measure latency without scoring")
	}

	grades, judgmentsMeta, err := loadJudgmentsMap(path, explicit)
	if err != nil {
		return nil, err
	}
	currentPool, err := pool.ReadPoolFile(tr.Pool)
	if err != nil {
		return nil, fmt.Errorf("read the track's pool to check what %s was graded against: %w", path, err)
	}
	if err := checkPoolBinding(judgmentsMeta.PoolRunID, currentPool); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &runJudgments{
		path:     path,
		grades:   grades,
		meta:     judgmentsMeta,
		coverage: poolCoverage(currentPool, grades),
		dropped:  currentPool.Meta.DroppedExecutions,
	}, nil
}

// judgmentCoverage counts the pool's (query, doc) pairs and how many of them
// the judgments leave without a grade.
type judgmentCoverage struct {
	pooled   int
	unjudged int
}

// poolCoverage checks the judgments against the pool itself rather than the
// judge's own count, which an interrupted run leaves stale and which cannot
// see queries the judge never reached.
func poolCoverage(pf *pool.PoolFile, grades map[string]map[string]int) judgmentCoverage {
	var c judgmentCoverage
	for _, entry := range pf.Queries {
		for _, doc := range entry.Docs {
			c.pooled++
			if _, graded := grades[entry.QueryID][doc.DocID.String()]; !graded {
				c.unjudged++
			}
		}
	}
	return c
}

// runFailures lists why a finished run cannot stand as the track's result.
func runFailures(result *runner.BenchmarkResult, judgments *runJudgments) []string {
	var failures []string
	if cells := measuredDroppedCells(result, judgments.dropped); len(cells) > 0 {
		failures = append(failures, fmt.Sprintf("the pool holds no documents for %d measured (query, engine) executions, so their hits score as non-relevant: %s; re-pool without --allow-partial",
			len(cells), strings.Join(cells, ", ")))
	}
	if cells := erroredCells(result); len(cells) > 0 {
		failures = append(failures, fmt.Sprintf("%d (query, engine) executions errored: %s", len(cells), strings.Join(cells, ", ")))
	}
	if judgments.coverage.unjudged > 0 {
		failures = append(failures, fmt.Sprintf("the judgments leave %d of %d pooled docs unjudged; finish judging (bench judge --resume)",
			judgments.coverage.unjudged, judgments.coverage.pooled))
	}
	return failures
}

// measuredDroppedCells lists the dropped pool cells this run executed.
func measuredDroppedCells(result *runner.BenchmarkResult, dropped []meta.DroppedExecution) []string {
	var cells []string
	for _, d := range dropped {
		for _, jr := range result.Jobs {
			if _, ran := jr.Results[d.QueryID][d.Engine]; ran {
				cells = append(cells, d.QueryID+"/"+d.Engine)
				break
			}
		}
	}
	return cells
}

func erroredCells(result *runner.BenchmarkResult) []string {
	var cells []string
	for _, jr := range result.Jobs {
		for _, queryID := range jr.QueryOrder {
			for _, engineName := range jr.EngineNames {
				if qr, ok := jr.Results[queryID][engineName]; ok && qr.Error != nil {
					cells = append(cells, fmt.Sprintf("%s:%s/%s", jr.JobName, queryID, engineName))
				}
			}
		}
	}
	return cells
}

// loadJudgmentsMap reads a judgments YAML and flattens to a runner-friendly
// map[queryID][docID]grade. Filters out unjudged entries (grade < 0). A missing
// file is an error; explicit says whether it came from --judgments or the
// spec default, which only changes the advice.
func loadJudgmentsMap(path string, explicit bool) (map[string]map[string]int, *meta.Meta, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if explicit {
			return nil, nil, fmt.Errorf("--judgments file not found: %s\n"+
				"  • check the strategy name (lexical, claude-cli, claude-api, manual)\n"+
				"  • or pass an explicit path to an existing YAML\n"+
				"  • or pass --latency-only to measure latency without scoring", path)
		}
		return nil, nil, fmt.Errorf("spec.defaults.judgments file not found: %s\n"+
			"  • judge the track first (bench judge)\n"+
			"  • or pass --latency-only to measure latency without scoring", path)
	}
	jf, err := judgment.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("load judgments %s: %w", path, err)
	}
	out := make(map[string]map[string]int, len(jf.Queries))
	for _, qe := range jf.Queries {
		inner := make(map[string]int, len(qe.Docs))
		for _, d := range qe.Docs {
			if d.Grade < 0 {
				continue
			}
			inner[d.DocID.String()] = d.Grade
		}
		out[qe.QueryID] = inner
	}
	return out, &jf.Meta, nil
}

// checkPoolBinding fails unless judgments graded against judgedPoolRunID
// belong to the current pool. A regenerated pool holds docs the judgments never
// saw, which would score as non-relevant.
func checkPoolBinding(judgedPoolRunID string, current *pool.PoolFile) error {
	if judgedPoolRunID == "" {
		return errors.New("judgments carry no pool_run_id, so the pool they were graded against is unknown; re-judge the track (bench judge)")
	}
	if judgedPoolRunID != current.Meta.RunID {
		return fmt.Errorf("judgments were graded against pool %s, but the current pool is %s; re-judge the track (bench judge)",
			judgedPoolRunID, current.Meta.RunID)
	}
	return nil
}

// collectCorpusInfo counts each engine's corpus and fails when an engine
// cannot be counted, is empty, or disagrees with the others, since their
// metrics would then describe different collections.
func collectCorpusInfo(ctx context.Context, executors map[string]engine.Executor) (report.CorpusInfo, error) {
	counts := make(map[string]int64)
	for name, exec := range executors {
		counter, ok := exec.(engine.CorpusCounter)
		if !ok {
			continue
		}
		n, err := counter.CorpusCount(ctx)
		if err != nil {
			return report.CorpusInfo{}, fmt.Errorf("count the corpus of engine %q: %w", name, err)
		}
		counts[name] = n
	}
	if len(counts) == 0 {
		return report.CorpusInfo{}, nil
	}

	var empty []string
	for _, name := range slices.Sorted(maps.Keys(counts)) {
		if counts[name] == 0 {
			empty = append(empty, name)
		}
	}
	if len(empty) > 0 {
		return report.CorpusInfo{}, fmt.Errorf("engines %s index 0 documents; load the corpus before running", strings.Join(empty, ", "))
	}

	var first int64
	seen := false
	for _, n := range counts {
		if !seen {
			first, seen = n, true
			continue
		}
		if n != first {
			return report.CorpusInfo{}, fmt.Errorf("engines index different document counts %v, so their metrics are not comparable", counts)
		}
	}
	return report.CorpusInfo{PerEngine: counts, DocCount: first}, nil
}

// indexDescribeTimeout bounds each engine's provenance read, so an engine that
// does not answer delays the run by this much rather than stalling it.
const indexDescribeTimeout = 15 * time.Second

// collectIndexProvenance records, for every engine, how it indexed the corpus.
// A failed read is warned about and kept in the engine's block as its error.
func collectIndexProvenance(ctx context.Context, w io.Writer, executors map[string]engine.Executor) map[string]report.IndexProvenance {
	provenance := make(map[string]report.IndexProvenance, len(executors))
	for name, exec := range executors {
		describer, ok := exec.(engine.IndexDescriber)
		if !ok {
			provenance[name] = report.IndexProvenance{Error: "engine cannot describe its index"}
			continue
		}
		description, err := describeIndex(ctx, describer)
		if err != nil {
			printWarn(w, fmt.Sprintf("index provenance for %q not recorded: %v", name, err))
			provenance[name] = report.IndexProvenance{Error: err.Error()}
			continue
		}
		provenance[name] = report.IndexProvenance{IndexDescription: *description}
	}
	return provenance
}

func describeIndex(ctx context.Context, describer engine.IndexDescriber) (*engine.IndexDescription, error) {
	ctx, cancel := context.WithTimeout(ctx, indexDescribeTimeout)
	defer cancel()
	return describer.DescribeIndex(ctx)
}

func firstNonZero(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

// updateLatestPointer writes a small JSON file ({"path":"reports/x.json"})
// next to the latest report. Cheap and doesn't depend on symlinks (which break
// on Windows / some FUSE mounts).
func updateLatestPointer(latestPath, actualPath string) error {
	rel, err := filepath.Rel(filepath.Dir(latestPath), actualPath)
	if err != nil {
		rel = actualPath
	}
	data := []byte(fmt.Sprintf(`{"latest": %q}`+"\n", rel))
	return os.WriteFile(latestPath, data, 0644)
}
