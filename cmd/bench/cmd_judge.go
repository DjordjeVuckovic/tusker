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
	apiModel       string
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
  claude-cli  — invokes 'claude -p' per batch (Anthropic LLM-as-judge batched)
  claude-api  — Anthropic Messages API in batches (set ANTHROPIC_API_KEY)
  manual      — writes grade:-1 placeholders for hand grading

Output goes to tracks/<name>/trec/annotations.<strategy>.yaml by default.
Multiple strategies live side-by-side; switch which one bench run scores
against via --judgments <name>.

Resumable: re-run with the same --strategy and --resume to skip docs already
graded. Atomic writes mean Ctrl-C is safe.`,
		Example: `  bench judge fts_quality --strategy lexical
  bench judge fts_quality --strategy claude-api --batch 20 --resume
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
	cmd.Flags().StringVar(&f.apiModel, "api-model", "", "Anthropic model id")
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

	kind := judgment.StrategyKind(f.strategy)
	outPath := f.output
	if outPath == "" {
		outPath = tr.JudgmentsPath(string(kind))
	}

	// Stub-equivalent shortcut: manual strategy doesn't need PG or any
	// network. Just emit grade:-1 placeholders so a human can edit.
	if kind == judgment.StrategyManual {
		jf := buildManualJudgments(pf)
		jf.Meta = meta.New("judge")
		jf.Meta.Strategy = string(kind)
		jf.Meta.PoolRef = poolPath
		if err := judgment.WriteFile(jf, outPath); err != nil {
			return fmt.Errorf("write judgments: %w", err)
		}
		printDone(cmd.OutOrStdout(), fmt.Sprintf("Manual template written: %s  (queries=%d)", outPath, len(jf.Queries)))
		return nil
	}

	opts := judgment.StrategyOptions{
		APIKey:      envOrFlag("ANTHROPIC_API_KEY", f.apiKey),
		APIModel:    f.apiModel,
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

	writer := judgment.NewIncrementalWriter(outPath, strat.Name())
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

	// Final write with completed meta block.
	final := writer.Snapshot()
	final.Meta = meta.New("judge")
	final.Meta.Strategy = strat.Name()
	final.Meta.PoolRef = poolPath
	final.Meta.RelevanceScale = []int{0, 1, 2, 3}
	final.Meta.GradedCount = countGraded(final)
	// G6: capture the actual model the strategy used, not the CLI flag (which
	// is empty when the user relied on the default model).
	if mi, ok := strat.(judgment.ModelIdentifier); ok {
		final.Meta.JudgeModel = mi.ModelID()
	}
	// G7: stamp the prompt version so rubric drift is detectable on resume.
	final.Meta.JudgePromptVersion = judgment.PromptVersion
	if err := judgment.WriteFile(final, outPath); err != nil {
		return fmt.Errorf("finalise judgments: %w", err)
	}

	printDone(cmd.OutOrStdout(), fmt.Sprintf("Judgments written: %s  (strategy=%s  queries=%d  run_id=%s)",
		outPath, final.Strategy, len(final.Queries), final.Meta.RunID))
	return nil
}

// checkResumeCompat verifies that a prior judgments file is safe to resume with
// the given strategy. It rejects files produced by a different strategy, model,
// or grading-prompt version — mixing those in one file corrupts the grade set.
func checkResumeCompat(prior *judgment.File, strat judgment.Strategy) error {
	if prior.Strategy != "" && prior.Strategy != strat.Name() {
		return fmt.Errorf("--resume strategy mismatch: existing file is %q, --strategy is %q",
			prior.Strategy, strat.Name())
	}
	if mi, ok := strat.(judgment.ModelIdentifier); ok {
		if prior.Meta.JudgeModel != "" && prior.Meta.JudgeModel != mi.ModelID() {
			return fmt.Errorf(
				"--resume model mismatch: existing file used %q, current strategy uses %q\n"+
					"  • re-run without --resume to start fresh with the new model",
				prior.Meta.JudgeModel, mi.ModelID())
		}
	}
	if prior.Meta.JudgePromptVersion != "" && prior.Meta.JudgePromptVersion != judgment.PromptVersion {
		return fmt.Errorf(
			"--resume prompt version mismatch: existing file used prompt %q, current is %q\n"+
				"  • the grading rubric changed; re-run without --resume to re-grade cleanly",
			prior.Meta.JudgePromptVersion, judgment.PromptVersion)
	}
	return nil
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
		Strategy: string(judgment.StrategyManual),
		Queries:  make([]judgment.Entry, 0, len(pf.Queries)),
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
