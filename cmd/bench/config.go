package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/DjordjeVuckovic/tusker/internal/bench/engine"
	"github.com/DjordjeVuckovic/tusker/internal/bench/spec"
	"github.com/DjordjeVuckovic/tusker/internal/bench/trackctx"
	"github.com/DjordjeVuckovic/tusker/internal/embedding"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/storage/factory"
)

func createExecutors(ctx context.Context, bs *spec.BenchSpec) (map[string]engine.Executor, func(), error) {
	return engine.CreateFromSpec(ctx, bs.Engines)
}

// buildQueryVectorStore builds the vector store pool/run use to embed queries
// for vector/hybrid tracks (PG precedence: it borrows a postgres engine's
// connection). Returns (nil, nil) when EMBEDDING_BASE_URL is unset or the spec
// has no postgres engine — tracks without vector queries don't need it, and
// vector queries without it simply fail to resolve (logged per-engine).
func buildQueryVectorStore(ctx context.Context, bs *spec.BenchSpec) (storage.VectorStore, error) {
	baseURL := os.Getenv("EMBEDDING_BASE_URL")
	if baseURL == "" {
		return nil, nil
	}
	var pgConn string
	for _, eng := range bs.Engines {
		if eng.Type == "postgres" {
			pgConn = eng.Connection
			break
		}
	}
	if pgConn == "" {
		return nil, nil
	}
	client, err := embedding.NewOllamaClient(baseURL)
	if err != nil {
		return nil, fmt.Errorf("embedding client: %w", err)
	}
	return factory.NewVectorStore(ctx, factory.VectorStoreConfig{
		PgConnStr:       pgConn,
		EmbeddingClient: client,
		Model:           os.Getenv("EMBEDDING_MODEL"),
	})
}

// requireEmbedder fails when the track's kind needs a live query embedder but
// none was built (EMBEDDING_BASE_URL unset or no postgres engine). This turns
// what used to be N per-query "missing precomputed" warnings into one clear,
// up-front error for semantic/hybrid tracks.
func requireEmbedder(bs *spec.BenchSpec, store storage.VectorStore) error {
	if !bs.Kind.RequiresEmbedder() || store != nil {
		return nil
	}
	return fmt.Errorf("kind %q requires an embedder: set EMBEDDING_BASE_URL and ensure a postgres engine is configured", bs.Kind)
}

// printSpecWarnings emits load-time advisories (e.g. kind omitted) once.
func printSpecWarnings(w io.Writer, bs *spec.BenchSpec) {
	for _, msg := range bs.Warnings {
		printWarn(w, msg)
	}
}

func parseKList(raw string) ([]int, error) {
	parts := strings.Split(raw, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		var v int
		if _, err := fmt.Sscanf(strings.TrimSpace(p), "%d", &v); err != nil {
			return nil, fmt.Errorf("invalid k value %q: %w", p, err)
		}
		if v <= 0 {
			return nil, fmt.Errorf("k value must be positive, got %d", v)
		}
		out = append(out, v)
	}
	return out, nil
}

func envOrFlag(envKey, flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	return os.Getenv(envKey)
}

// trackArg picks up the track from a flag or first positional arg. The CLI
// allows either form: `bench pool fts_quality` or `bench pool --track fts_quality`.
func trackArg(flag string, args []string) string {
	if flag != "" {
		return flag
	}
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

// forEachTrack runs fn once per track. A glob arg (news/*) fans out across every
// matching track; anything else is a single track. Grouping is explicit — only a
// glob expands. Path overrides (--spec/--suite/--pool/--output) target one track,
// so combining them with a glob is rejected. In group mode a per-track failure is
// logged and the loop continues; the aggregate error names every track that failed.
func forEachTrack(w io.Writer, in trackctx.Inputs, fn func(*trackctx.Track) error) error {
	if !trackctx.IsPattern(in.TrackArg) {
		tr, err := trackctx.Resolve(in)
		if err != nil {
			return err
		}
		return fn(tr)
	}

	if in.SpecPath != "" || in.SuitePath != "" || in.PoolPath != "" || in.OutputPath != "" {
		return fmt.Errorf("--spec/--suite/--pool/--output cannot be combined with a glob pattern %q", in.TrackArg)
	}
	tracks, err := trackctx.ResolveGlob(in.TrackArg)
	if err != nil {
		return err
	}
	if len(tracks) == 1 {
		return fn(tracks[0])
	}

	var failed []string
	for _, tr := range tracks {
		fmt.Fprintf(w, "\n%s %s\n", cBold.Sprint("━━"), cBold.Sprint(tr.Name()))
		if err := fn(tr); err != nil {
			printWarn(w, fmt.Sprintf("%s failed: %v", tr.Name(), err))
			failed = append(failed, tr.Name())
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d of %d track(s) failed: %s", len(failed), len(tracks), strings.Join(failed, ", "))
	}
	return nil
}
