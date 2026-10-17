package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/DjordjeVuckovic/tusker/internal/cli"
	"github.com/DjordjeVuckovic/tusker/internal/datapackage"
	"github.com/DjordjeVuckovic/tusker/internal/embedding"
	"github.com/DjordjeVuckovic/tusker/internal/ingest/reader"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/storage/factory"
	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/DjordjeVuckovic/tusker/internal/types/language"
	"github.com/DjordjeVuckovic/tusker/pkg/config/env"
	"github.com/google/uuid"
	"github.com/parquet-go/parquet-go"
	"github.com/spf13/cobra"
)

const defaultLoadBatchSize = 5_000

type loadOptions struct {
	Engine     string
	Canonical  string
	Embeddings string
	SkipIndex  bool
	Trial      bool
	BatchSize  int
	RecordsDir string
}

func newLoadCmd() *cobra.Command {
	var opts loadOptions
	cmd := &cobra.Command{
		Use:   "load",
		Short: "Load a canonical package, and optionally its embeddings, into one engine",
		Long: `load verifies a canonical package, and an embeddings package built from it,
loads both into an empty engine and builds the engine's indexes after the data.
The load record goes to the engine and to datasets/<ds>/loads/<engine>/.

The connection comes from the environment: PG_CONNECTION_STRING for native,
paradedb and tiger, ES_ADDRESSES and ES_INDEX_NAME for es.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := env.LoadDotEnv(os.Getenv("ENV")); err != nil {
				slog.Info("Skipping .env environment variables", "error", err)
			}
			return runLoad(cmd.Context(), cmd.OutOrStdout(), opts)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.Engine, "engine", "", "engine to load: native, paradedb, tiger or es")
	flags.StringVar(&opts.Canonical, "canonical", "", "canonical package descriptor: a path, https:// or s3://")
	flags.StringVar(&opts.Embeddings, "embeddings", "", "embeddings package descriptor built from the canonical package")
	flags.BoolVar(&opts.SkipIndex, "skip-index", false, "stop after the data: build no index and write no load record")
	flags.BoolVar(&opts.Trial, "trial", false, "accept an embeddings package from a --limit trial")
	flags.IntVar(&opts.BatchSize, "batch-size", defaultLoadBatchSize, "rows per COPY or bulk request, the same on every engine")
	flags.StringVar(&opts.RecordsDir, "records-dir", "", "directory for the load record; derived from a local canonical path by default")
	_ = cmd.MarkFlagRequired("engine")
	_ = cmd.MarkFlagRequired("canonical")
	return cmd
}

func runLoad(ctx context.Context, out io.Writer, opts loadOptions) error {
	engine, err := storage.ParseEngine(opts.Engine)
	if err != nil {
		return err
	}
	recordsDir := opts.RecordsDir
	if recordsDir == "" && !opts.SkipIndex {
		if recordsDir, err = derivedRecordsDir(opts.Canonical, engine); err != nil {
			return err
		}
	}

	inputs, err := openLoadInputs(ctx, opts)
	if err != nil {
		return err
	}
	defer inputs.Close()

	storageCfg, err := factory.LoadEnvFor(engine.Type())
	if err != nil {
		return fmt.Errorf("load %s connection settings: %w", engine, err)
	}
	loader, release, err := factory.NewCorpusLoader(ctx, factory.LoaderConfig{Engine: engine, Pg: storageCfg.Pg, Es: storageCfg.Es})
	if err != nil {
		return err
	}
	defer release()

	cli.Header(out, "tusker load", inputs.fields(engine, opts)...)
	record, err := loader.LoadCorpus(ctx, inputs.corpus(ctx), storage.LoadOptions{BatchSize: opts.BatchSize, SkipIndex: opts.SkipIndex})
	if err != nil {
		return fmt.Errorf("load %s: %w", engine, err)
	}

	fields := []cli.Field{
		{Label: "articles", Value: fmt.Sprint(record.Articles)},
		{Label: "vectors", Value: fmt.Sprint(record.Vectors)},
	}
	for _, stage := range record.Stages {
		fields = append(fields, cli.Field{Label: stage.Name, Value: fmt.Sprintf("%.1fs", stage.Seconds)})
	}
	if opts.SkipIndex {
		cli.Summary(out, "Corpus loaded without indexes", fields...)
		cli.Warn(out, "no load record written; build the indexes with make migrate-indexes-"+string(engine))
		return nil
	}
	path, err := writeLoadRecord(recordsDir, record)
	if err != nil {
		return err
	}
	cli.Summary(out, "Corpus loaded", append(fields, cli.Field{Label: "record", Value: path})...)
	return nil
}

// derivedRecordsDir maps datasets/<ds>/canonical/<version>/ to
// datasets/<ds>/loads/<engine>/.
func derivedRecordsDir(canonical string, engine storage.Engine) (string, error) {
	if strings.Contains(canonical, "://") {
		return "", fmt.Errorf("canonical package %s is remote; pass --records-dir", canonical)
	}
	dir := canonical
	if filepath.Ext(dir) == ".json" {
		dir = filepath.Dir(dir)
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", canonical, err)
	}
	if filepath.Base(filepath.Dir(dir)) != "canonical" {
		return "", fmt.Errorf("canonical package %s is not under datasets/<ds>/canonical/<version>/; pass --records-dir", canonical)
	}
	return filepath.Join(filepath.Dir(filepath.Dir(dir)), "loads", string(engine)), nil
}

// writeLoadRecord writes record as <startedAt>.json and never replaces an
// existing record.
func writeLoadRecord(dir string, record storage.LoadRecord) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode load record: %w", err)
	}
	// No ':' in the name, so the record checks out on every filesystem.
	path := filepath.Join(dir, record.StartedAt.UTC().Format("2006-01-02T15-04-05.000000Z")+".json")
	tmp, err := os.CreateTemp(dir, ".load-record-*")
	if err != nil {
		return "", fmt.Errorf("create load record: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return "", fmt.Errorf("write load record: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("write load record: %w", err)
	}
	if err := os.Link(tmp.Name(), path); err != nil {
		return "", fmt.Errorf("publish load record %s: %w", path, err)
	}
	return path, nil
}

// loadInputs are the verified packages a load reads.
type loadInputs struct {
	canonical      *datapackage.Opened
	articles       *datapackage.ResourceFile
	articlesFormat string
	articlesHash   string
	embeddings     *datapackage.Opened
	vectors        *datapackage.ResourceFile
	vectorsHash    string
}

// openLoadInputs checks both descriptors before reading any resource, then
// verifies each resource against its hash.
func openLoadInputs(ctx context.Context, opts loadOptions) (*loadInputs, error) {
	inputs := &loadInputs{}
	if err := inputs.open(ctx, opts); err != nil {
		inputs.Close()
		return nil, err
	}
	return inputs, nil
}

func (in *loadInputs) open(ctx context.Context, opts loadOptions) error {
	var err error
	if in.canonical, err = datapackage.Open(ctx, opts.Canonical); err != nil {
		return fmt.Errorf("open canonical package: %w", err)
	}
	canonical := in.canonical.Package
	if canonical.ArticleSchema != datapackage.ArticleSchema {
		return fmt.Errorf("canonical package %s has article schema %q, want %q", canonical.Name, canonical.ArticleSchema, datapackage.ArticleSchema)
	}
	if _, err := language.Parse(string(canonical.Language)); err != nil {
		return fmt.Errorf("canonical package %s: %w", canonical.Name, err)
	}
	articles, err := describedResource(canonical, datapackage.ArticlesResource)
	if err != nil {
		return err
	}
	in.articlesFormat, in.articlesHash = articles.Format, articles.Hash

	if opts.Embeddings != "" {
		if in.embeddings, err = datapackage.Open(ctx, opts.Embeddings); err != nil {
			return fmt.Errorf("open embeddings package: %w", err)
		}
		if err := checkEmbeddings(in.embeddings.Package, articles.Hash, opts.Trial); err != nil {
			return err
		}
		vectors, err := describedResource(in.embeddings.Package, datapackage.EmbeddingsResource)
		if err != nil {
			return err
		}
		in.vectorsHash = vectors.Hash
	}

	if in.articles, err = in.canonical.Resource(ctx, datapackage.ArticlesResource); err != nil {
		return fmt.Errorf("verify canonical package %s: %w", canonical.Name, err)
	}
	if in.embeddings != nil {
		if in.vectors, err = in.embeddings.Resource(ctx, datapackage.EmbeddingsResource); err != nil {
			return fmt.Errorf("verify embeddings package %s: %w", in.embeddings.Package.Name, err)
		}
	}
	return nil
}

func describedResource(pkg datapackage.Package, name string) (datapackage.Resource, error) {
	for _, r := range pkg.Resources {
		if r.Name == name {
			return r, nil
		}
	}
	return datapackage.Resource{}, &datapackage.ResourceNotFoundError{Package: pkg.Name, Name: name}
}

// checkEmbeddings refuses vectors not embedded from the canonical resource
// being loaded, and a --limit trial unless the caller asked for one.
func checkEmbeddings(pkg datapackage.Package, canonicalHash string, trial bool) error {
	if pkg.Embedding == nil || pkg.Embedding.Tag == "" {
		return fmt.Errorf("embeddings package %s does not record its model tag", pkg.Name)
	}
	if pkg.Canonical == nil {
		return fmt.Errorf("embeddings package %s does not name its canonical package", pkg.Name)
	}
	if pkg.Canonical.ResourceHash != canonicalHash {
		return fmt.Errorf("embeddings package %s was built from canonical resource %s, not %s",
			pkg.Name, pkg.Canonical.ResourceHash, canonicalHash)
	}
	if limit := pkg.Embedding.Limit; limit != nil && !trial {
		return fmt.Errorf("embeddings package %s is a trial of its first %d rows; pass --trial to load it", pkg.Name, *limit)
	}
	return nil
}

func (in *loadInputs) corpus(ctx context.Context) storage.Corpus {
	canonical := in.canonical.Package
	c := storage.Corpus{
		Articles:  canonicalArticles(ctx, in.articles, in.articlesFormat),
		Language:  canonical.Language,
		Canonical: storage.PackageRef{Name: canonical.Name, Version: canonical.Version, ResourceHash: in.articlesHash},
	}
	if in.embeddings != nil {
		embeddings := in.embeddings.Package
		c.ModelTag = embeddings.Embedding.Tag
		c.Vectors = packageVectors(ctx, in.vectors, c.ModelTag)
		c.Embeddings = &storage.PackageRef{Name: embeddings.Name, Version: embeddings.Version, ResourceHash: in.vectorsHash}
	}
	return c
}

func (in *loadInputs) fields(engine storage.Engine, opts loadOptions) []cli.Field {
	fields := []cli.Field{
		{Label: "engine", Value: string(engine)},
		{Label: "canonical", Value: in.canonical.Package.Name + "@" + in.canonical.Package.Version},
	}
	if in.embeddings != nil {
		fields = append(fields, cli.Field{Label: "embeddings", Value: in.embeddings.Package.Name + "@" + in.embeddings.Package.Version})
	}
	indexes := "built after the data"
	if opts.SkipIndex {
		indexes = "skipped"
	}
	return append(fields, cli.IntField("batch", opts.BatchSize), cli.Field{Label: "indexes", Value: indexes})
}

func (in *loadInputs) Close() {
	for _, f := range []*datapackage.ResourceFile{in.articles, in.vectors} {
		if f != nil {
			_ = f.Close()
		}
	}
}

// canonicalArticles reads the canonical resource in file order, which is the
// order the embeddings follow.
func canonicalArticles(ctx context.Context, file *datapackage.ResourceFile, format string) iter.Seq2[document.Article, error] {
	return func(yield func(document.Article, error) bool) {
		var source reader.RawParallelReader
		switch format {
		case "parquet":
			parquetReader, err := reader.NewParquetReader(file, file.Size())
			if err != nil {
				yield(document.Article{}, fmt.Errorf("open canonical parquet: %w", err))
				return
			}
			source = parquetReader
		case "jsonl":
			source = reader.NewJSONLReader(file)
		default:
			yield(document.Article{}, fmt.Errorf("canonical resource format %q: want parquet or jsonl", format))
			return
		}

		readCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		// A single worker keeps the file's row order.
		records, err := source.ReadParallel(readCtx, 1)
		if err != nil {
			yield(document.Article{}, fmt.Errorf("read canonical resource: %w", err))
			return
		}
		mapper := reader.NewArticleDirectMapper()
		row := 0
		for record := range records {
			if record.Err != nil {
				yield(document.Article{}, fmt.Errorf("read canonical row %d: %w", row, record.Err))
				return
			}
			article, err := mapper.Map(record.Record)
			if err != nil {
				yield(document.Article{}, fmt.Errorf("canonical row %d: %w", row, err))
				return
			}
			if !yield(article, nil) {
				return
			}
			row++
		}
		// The reader also closes its channel on cancel, which must not read as
		// the end of the corpus.
		if err := ctx.Err(); err != nil {
			yield(document.Article{}, err)
		}
	}
}

// embeddingRow is one row of an embeddings resource. `,list` matches the
// 3-level LIST encoding pyarrow writes, which parquet-go otherwise decodes to
// nil.
type embeddingRow struct {
	ID        string    `parquet:"id"`
	Embedding []float32 `parquet:"embedding,list"`
}

const vectorReadBatch = 256

// packageVectors reads the embeddings resource in row order, tagging each
// vector with the package's model tag.
func packageVectors(ctx context.Context, file *datapackage.ResourceFile, tag string) iter.Seq2[embedding.Vec, error] {
	return func(yield func(embedding.Vec, error) bool) {
		rows := parquet.NewGenericReader[embeddingRow](file)
		defer rows.Close()
		position := 0
		for {
			if err := ctx.Err(); err != nil {
				yield(embedding.Vec{}, err)
				return
			}
			// A fresh batch per read: the caller may keep the vectors.
			batch := make([]embeddingRow, vectorReadBatch)
			n, readErr := rows.Read(batch)
			for _, row := range batch[:n] {
				id, err := uuid.Parse(row.ID)
				if err != nil {
					yield(embedding.Vec{}, fmt.Errorf("embeddings row %d: %w", position, err))
					return
				}
				if !yield(embedding.Vec{ID: id, Model: tag, Embedding: row.Embedding}, nil) {
					return
				}
				position++
			}
			if errors.Is(readErr, io.EOF) {
				return
			}
			if readErr != nil {
				yield(embedding.Vec{}, fmt.Errorf("read embeddings row %d: %w", position, readErr))
				return
			}
		}
	}
}
