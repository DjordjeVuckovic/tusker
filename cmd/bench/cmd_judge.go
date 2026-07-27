package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/DjordjeVuckovic/tusker/internal/bench/judgment"
	"github.com/DjordjeVuckovic/tusker/internal/bench/meta"
	"github.com/DjordjeVuckovic/tusker/internal/bench/pool"
	"github.com/DjordjeVuckovic/tusker/internal/bench/trackctx"
	"github.com/DjordjeVuckovic/tusker/internal/embedding"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/storage/factory"
	"github.com/DjordjeVuckovic/tusker/internal/storage/pg"
	"github.com/spf13/cobra"
)

type judgeFlags struct {
	trackArg       string
	poolPath       string
	output         string
	strategy       string
	pg             string
	concurrency    int
	batchSize      int
	resume         bool
	apiKey         string
	provider       string
	model          string
	apiBaseURL     string
	cliBinary      string
	embeddingBase  string
	embeddingModel string
}

func newJudgeCmd() *cobra.Command {
	var f judgeFlags
	cmd := &cobra.Command{
		Use:   "judge [track]",
		Short: "Grade a pool file with the chosen strategy",
		Long: `Grades every (query, doc) pair in the track's pool using one of:

  lexical     — deterministic token-overlap baseline (no network, no LLM)
  bm25        — pool-local Okapi BM25 (no network; rewards rare terms)
  vector      — cosine similarity; doc vectors from PG, query embedded via
                Ollama (needs --pg + EMBEDDING_BASE_URL)
  hybrid      — BM25 + vector fusion (needs --pg + EMBEDDING_BASE_URL)
  llm         — LLM-as-judge, batched; pick the endpoint with --provider
  manual      — writes grade:-1 placeholders for hand grading

The llm strategy is vendor-neutral: --provider names the endpoint
(` + strings.Join(judgment.KnownProviders(), ", ") + `), and registering a new
provider is all it takes to add Codex or ChatGPT.

--model accepts a full model id or a shorthand (` + strings.Join(judgment.ClaudeModelAliases(), ", ") + `)
and defaults to ` + judgment.DefaultJudgeModel + `. The resolved id, the
provider and the prompt version are recorded in the artifact's judge block, so
qrels always name the grader that produced them.

Output goes to tracks/<name>/trec/annotations.<name>.yaml, where <name> is the
strategy for heuristic judges and the provider for llm judges.
Multiple strategies live side-by-side; switch which one bench run scores
against via --judgments <name>.

Resumable: re-run with the same --strategy and --resume to skip docs already
graded. Atomic writes mean Ctrl-C is safe.`,
		Example: `  bench judge fts_quality --strategy lexical
  bench judge fts_quality --strategy llm --provider claude-cli
  bench judge fts_quality --strategy llm --provider claude-api --model sonnet --batch 20 --resume
  bench judge --pool /tmp/p.yaml --strategy lexical --output /tmp/a.yaml`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeJudge(cmd, f, args)
		},
	}
	cmd.Flags().StringVar(&f.trackArg, "track", "", "Track name or path")
	cmd.Flags().StringVar(&f.poolPath, "pool", "", "Override pool YAML path")
	cmd.Flags().StringVar(&f.output, "output", "", "Override annotations output path")
	cmd.Flags().StringVar(&f.strategy, "strategy", string(judgment.StrategyLexical), "Judge strategy")
	cmd.Flags().StringVar(&f.pg, "pg", "", "Postgres connection (or set PG_CONNECTION_STRING)")
	cmd.Flags().IntVar(&f.concurrency, "concurrency", 4, "Parallel Grade calls (per-doc strategies)")
	cmd.Flags().IntVar(&f.batchSize, "batch", 0, "Override LLM batch size (0 = strategy default)")
	cmd.Flags().BoolVar(&f.resume, "resume", false, "Skip docs already graded in --output")
	cmd.Flags().StringVar(&f.apiKey, "api-key", "", "Anthropic API key (or set ANTHROPIC_API_KEY)")
	cmd.Flags().StringVar(&f.provider, "provider", string(judgment.DefaultProvider),
		"LLM endpoint for --strategy llm: "+strings.Join(judgment.KnownProviders(), " | "))
	cmd.Flags().StringVar(&f.model, "model", "",
		"Judge model id or alias ("+strings.Join(judgment.ClaudeModelAliases(), " | ")+"); default "+judgment.DefaultJudgeModel)
	cmd.Flags().StringVar(&f.apiBaseURL, "api-base", "", "Anthropic API base URL")
	cmd.Flags().StringVar(&f.cliBinary, "cli-binary", "", "claude CLI binary path")
	cmd.Flags().StringVar(&f.embeddingBase, "embedding-base", "", "Embedding endpoint for vector/hybrid (or EMBEDDING_BASE_URL)")
	cmd.Flags().StringVar(&f.embeddingModel, "embedding-model", "", "Embedding model for vector/hybrid (or EMBEDDING_MODEL)")
	return cmd
}

func executeJudge(cmd *cobra.Command, f judgeFlags, args []string) error {
	return forEachTrack(cmd.OutOrStdout(), trackctx.Inputs{
		TrackArg:   trackArg(f.trackArg, args),
		PoolPath:   f.poolPath,
		OutputPath: f.output,
	}, func(tr *trackctx.Track) error {
		return judgeTrack(cmd, f, tr)
	})
}

func judgeTrack(cmd *cobra.Command, f judgeFlags, tr *trackctx.Track) error {
	poolPath := f.poolPath
	if poolPath == "" {
		poolPath = tr.Pool
	}
	pf, err := pool.ReadPoolFile(poolPath)
	if err != nil {
		return fmt.Errorf("read pool: %w", err)
	}

	kind, provider, err := resolveJudge(f)
	if err != nil {
		return err
	}
	// Heuristic judges file under their strategy; llm judges file under their
	// provider, so two vendors can grade the same track side by side.
	outPath := f.output
	if outPath == "" {
		outPath = tr.JudgmentsPath(judgment.JudgmentSetName(kind, provider))
	}

	// Stub-equivalent shortcut: manual strategy doesn't need PG or any
	// network. Just emit grade:-1 placeholders so a human can edit.
	if kind == judgment.StrategyManual {
		jf := buildManualJudgments(pf)
		judge := judgment.NewManualStrategy().Describe()
		jf.Meta = meta.New("judge")
		jf.Meta.Judge = &judge
		jf.Meta.PoolRef = poolPath
		if err := judgment.WriteFile(jf, outPath); err != nil {
			return fmt.Errorf("write judgments: %w", err)
		}
		printDone(cmd.OutOrStdout(), fmt.Sprintf("Manual template written: %s  (queries=%d)", outPath, len(jf.Queries)))
		return nil
	}

	opts := judgment.StrategyOptions{
		APIKey:      envOrFlag("ANTHROPIC_API_KEY", f.apiKey),
		Provider:    provider,
		Model:       f.model,
		APIBaseURL:  f.apiBaseURL,
		CLIBinary:   f.cliBinary,
		Concurrency: f.concurrency,
	}
	if kind == judgment.StrategyVector || kind == judgment.StrategyHybrid {
		store, model, err := buildVectorStore(cmd.Context(), f)
		if err != nil {
			return err
		}
		opts.VectorStore = store
		opts.EmbeddingModel = model
	}

	strat, err := judgment.NewStrategy(kind, opts)
	if err != nil {
		return err
	}

	reader, err := openArticleReader(cmd, f.pg)
	if err != nil {
		return err
	}

	judge := strat.Describe()
	writer := judgment.NewIncrementalWriter(outPath, judge)
	var prior *judgment.File
	if f.resume {
		prior, err = writer.LoadPrior()
		if err != nil {
			return fmt.Errorf("load prior judgments: %w", err)
		}
		if prior != nil {
			if err := checkResumeCompat(prior, strat); err != nil {
				return err
			}
			printWarn(cmd.OutOrStdout(), fmt.Sprintf("Resume: loaded %d prior queries from %s", len(prior.Queries), outPath))
		}
	}

	jrunner := judgment.NewRunner(judgment.RunnerConfig{
		Strategy:    strat,
		Reader:      reader,
		Concurrency: f.concurrency,
		BatchSize:   f.batchSize,
		Existing:    prior,
		Sink:        writer.Append,
		OnQueryStart: func(qid string, total, skipped int) {
			if skipped > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "%s grading %d docs %s\n",
					cCyan.Sprintf("[%s]", qid), total-skipped,
					cDim.Sprintf("(%d already done)", skipped))
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%s grading %d docs\n",
					cCyan.Sprintf("[%s]", qid), total)
			}
		},
		OnBatch: func(bp judgment.BatchProgress) {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s batch %d/%d: graded=%d missing=%d %s\n",
				cDim.Sprint("└"),
				bp.BatchIdx, bp.BatchN, bp.Graded, bp.Missing,
				cDim.Sprint(formatHistogram(bp.Histogram)))
		},
		OnQueryDone: func(qp judgment.QueryProgress) {
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s graded=%d skipped=%d unjudged=%d %s\n",
				cCyan.Sprintf("[%s]", qp.QueryID),
				cOK.Sprint("done:"),
				qp.Graded, qp.Skipped, qp.Unjudged,
				cDim.Sprint(formatHistogram(qp.Histogram)))
		},
	})

	if _, err := jrunner.Run(cmd.Context(), pf); err != nil {
		return fmt.Errorf("judge run: %w", err)
	}

	// Final write with completed meta block. The judge descriptor captures what
	// the strategy actually resolved to — including the model, which is empty on
	// the flag whenever the user relied on the default — and the prompt version,
	// so rubric drift is detectable on resume.
	final := writer.Snapshot()
	final.Meta = meta.New("judge")
	final.Meta.Judge = &judge
	final.Meta.PoolRef = poolPath
	final.Meta.RelevanceScale = []int{0, 1, 2, 3}
	final.Meta.GradedCount = countGraded(final)
	if err := judgment.WriteFile(final, outPath); err != nil {
		return fmt.Errorf("finalise judgments: %w", err)
	}

	printDone(cmd.OutOrStdout(), fmt.Sprintf("Judgments written: %s  (judge=%s  queries=%d  run_id=%s)",
		outPath, judge, len(final.Queries), final.Meta.RunID))
	return nil
}

// resolveJudge turns --strategy/--provider into the pair the strategy factory
// needs. --provider applies only to the llm strategy; naming a judgment set
// ("claude-cli") in --strategy is redirected to the explicit form rather than
// silently accepted, so there is one way to write a judge.
func resolveJudge(f judgeFlags) (judgment.StrategyKind, judgment.Provider, error) {
	kind := judgment.StrategyKind(f.strategy)
	switch kind {
	case judgment.StrategyLLM:
		return kind, judgment.Provider(f.provider), nil
	case judgment.StrategyLexical, judgment.StrategyBM25, judgment.StrategyVector,
		judgment.StrategyHybrid, judgment.StrategyManual:
		return kind, "", nil
	}

	// A provider name in --strategy is the old spelling. Redirect rather than
	// accept it, so there is exactly one way to write a judge.
	if _, p, err := judgment.ParseSelector(f.strategy); err == nil {
		return "", "", fmt.Errorf("%q names a provider, not a strategy — use --strategy llm --provider %s",
			f.strategy, p)
	}
	return "", "", fmt.Errorf("unknown strategy %q (known: %s)",
		f.strategy, strings.Join(judgment.KnownJudgeStrategies(), ", "))
}

// checkResumeCompat verifies that a prior judgments file is safe to resume with
// the given strategy. Any difference in the judge block — strategy, provider,
// model or prompt version — means the two runs disagree on who is grading, and
// appending would corrupt the set.
func checkResumeCompat(prior *judgment.File, strat judgment.Strategy) error {
	was, now := prior.Judge(), strat.Describe()
	if (was == meta.Judge{}) || was.Equal(now) {
		return nil
	}
	return fmt.Errorf(
		"--resume judge mismatch: existing file was graded by %q, this run uses %q\n"+
			"  • re-run without --resume to re-grade cleanly under the new judge",
		was, now)
}

// buildVectorStore constructs the engine-agnostic vector store for the
// vector/hybrid judges (PG precedence). Query text is embedded via local
// Ollama; document vectors are read from the store — no document re-embedding.
func buildVectorStore(ctx context.Context, f judgeFlags) (storage.VectorStore, string, error) {
	pgConn := envOrFlag("PG_CONNECTION_STRING", f.pg)
	if pgConn == "" {
		return nil, "", fmt.Errorf("vector/hybrid judging requires --pg or PG_CONNECTION_STRING")
	}
	baseURL := envOrFlag("EMBEDDING_BASE_URL", f.embeddingBase)
	if baseURL == "" {
		return nil, "", fmt.Errorf("vector/hybrid judging requires --embedding-base or EMBEDDING_BASE_URL (ollama endpoint)")
	}
	client, err := embedding.NewOllamaClient(baseURL)
	if err != nil {
		return nil, "", fmt.Errorf("embedding client: %w", err)
	}
	model := envOrFlag("EMBEDDING_MODEL", f.embeddingModel)
	store, err := factory.NewVectorStore(ctx, factory.VectorStoreConfig{
		PgConnStr:       pgConn,
		EmbeddingClient: client,
		Model:           model,
	})
	if err != nil {
		return nil, "", err
	}
	if model == "" {
		model = embedding.DefaultModel
	}
	return store, model, nil
}

// openArticleReader creates a PG reader for article enrichment. Centralised so
// the no-key-needed case (manual strategy) can skip it cleanly.
func openArticleReader(cmd *cobra.Command, pgConn string) (storage.Reader, error) {
	conn := envOrFlag("PG_CONNECTION_STRING", pgConn)
	if conn == "" {
		return nil, fmt.Errorf("judge requires --pg or PG_CONNECTION_STRING for article enrichment")
	}
	reader, err := factory.NewReader(cmd.Context(), factory.StorageConfig{
		Type: storage.PG,
		Pg:   &pg.PoolConfig{ConnStr: conn},
	})
	if err != nil {
		return nil, fmt.Errorf("create reader: %w", err)
	}
	return reader, nil
}

func buildManualJudgments(pf *pool.PoolFile) *judgment.File {
	jf := &judgment.File{
		Queries: make([]judgment.Entry, 0, len(pf.Queries)),
	}
	for _, entry := range pf.Queries {
		docs := make([]judgment.GradedDoc, 0, len(entry.Docs))
		for _, d := range entry.Docs {
			docs = append(docs, judgment.GradedDoc{DocID: d.DocID, Grade: judgment.GradeUnjudged})
		}
		jf.Queries = append(jf.Queries, judgment.Entry{QueryID: entry.QueryID, Docs: docs})
	}
	return jf
}

func countGraded(jf *judgment.File) int {
	n := 0
	for _, qe := range jf.Queries {
		for _, d := range qe.Docs {
			if d.Grade >= 0 {
				n++
			}
		}
	}
	return n
}

func formatHistogram(h map[int]int) string {
	if len(h) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("(")
	for g := 3; g >= 0; g-- {
		if g < 3 {
			b.WriteString(" ")
		}
		fmt.Fprintf(&b, "%d:%d", g, h[g])
	}
	b.WriteString(")")
	return b.String()
}
