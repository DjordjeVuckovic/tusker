package ingest

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/DjordjeVuckovic/tusker/internal/datapackage"
	"github.com/DjordjeVuckovic/tusker/internal/ingest/reader"
	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/DjordjeVuckovic/tusker/pkg/apis/datamapping"
	"github.com/google/uuid"
)

// PreprocessOptions names the raw package to map, the mapping that maps it and
// the folder that receives canonical/<version>. An empty Version publishes
// datapackage.DefaultVersion.
type PreprocessOptions struct {
	Raw        *datapackage.Opened
	Mapping    *datamapping.DataMapper
	OutputRoot string
	Version    string
	ProducedBy datapackage.ProducedBy
}

// Preprocess maps every record of the raw package, in the raw descriptor's
// resource order, onto an article and publishes the canonical package to
// OutputRoot/<version>. When that version is already there, it is kept.
func Preprocess(ctx context.Context, opts PreprocessOptions) (datapackage.Published, error) {
	mapper, err := reader.NewArticleMapper(opts.Mapping)
	if err != nil {
		return datapackage.Published{}, fmt.Errorf("build article mapper: %w", err)
	}
	format, err := opts.Mapping.Output.ResolvedFormat()
	if err != nil {
		return datapackage.Published{}, err
	}
	lang, err := opts.Mapping.Constants.ResolvedLanguage()
	if err != nil {
		return datapackage.Published{}, err
	}

	raw := opts.Raw.Package
	pkg := datapackage.Package{
		Name:          canonicalName(raw.Name),
		ArticleSchema: datapackage.ArticleSchema,
		Language:      lang,
		IDStrategy:    idStrategyOf(opts.Mapping),
		ProducedBy:    &opts.ProducedBy,
		Raw:           &datapackage.RawRef{Name: raw.Name, Version: raw.Version},
		Sources:       httpsSources(raw.Sources),
		Resources: []datapackage.Resource{{
			Name:   datapackage.ArticlesResource,
			Path:   datapackage.ArticlesResource + "." + string(format),
			Format: string(format),
		}},
	}

	builder := &canonicalBuilder{
		raw:    opts.Raw,
		mapper: mapper,
		seen:   map[uuid.UUID]struct{}{},
		report: datapackage.Report{Dropped: map[string]int64{}},
	}
	return builder.publish(ctx, opts.OutputRoot, opts.Version, pkg)
}

// publish builds the package in a staging folder beside its version folder,
// which datapackage.Publish moves into place only once it is complete, so a
// failed or cancelled run leaves nothing behind.
func (b *canonicalBuilder) publish(ctx context.Context, outputRoot, version string, pkg datapackage.Package) (_ datapackage.Published, err error) {
	if err := os.MkdirAll(outputRoot, 0o755); err != nil {
		return datapackage.Published{}, fmt.Errorf("create canonical output folder: %w", err)
	}
	staging, err := os.MkdirTemp(outputRoot, ".staging-*")
	if err != nil {
		return datapackage.Published{}, fmt.Errorf("create staging package folder: %w", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(staging); removeErr != nil && err == nil {
			err = fmt.Errorf("remove staging package folder: %w", removeErr)
		}
	}()

	if err := b.writeArticlesFile(ctx, filepath.Join(staging, pkg.Resources[0].Path), datamapping.OutputFormat(pkg.Resources[0].Format)); err != nil {
		return datapackage.Published{}, err
	}
	pkg.Report = &b.report
	// MkdirTemp creates the folder 0700, and Publish renames this very folder.
	if err := os.Chmod(staging, 0o755); err != nil {
		return datapackage.Published{}, fmt.Errorf("open up staging package folder: %w", err)
	}
	published, err := datapackage.Publish(ctx, pkg, datapackage.PublishOptions{StagingDir: staging, ParentDir: outputRoot, Version: version})
	if err != nil {
		return datapackage.Published{}, fmt.Errorf("publish canonical package into %s: %w", outputRoot, err)
	}
	return published, nil
}

func canonicalName(rawName string) string {
	return strings.TrimSuffix(rawName, "-raw") + "-canonical"
}

func idStrategyOf(mapping *datamapping.DataMapper) *datapackage.IDStrategy {
	var urlSource string
	for _, fm := range mapping.FieldMappings {
		if fm.Target == datamapping.TargetURL {
			urlSource = fm.Source
		}
	}
	return &datapackage.IDStrategy{
		Kind:      datamapping.IdStrategyUuidV5,
		Source:    urlSource,
		Namespace: document.ArticleIDNamespace.String(),
	}
}

// httpsSources keeps the human provenance a canonical descriptor can carry:
// Data Package v2 forbids relative paths that leave the package folder.
func httpsSources(sources []datapackage.Source) []datapackage.Source {
	var kept []datapackage.Source
	for _, s := range sources {
		if strings.HasPrefix(s.Path, "https://") {
			kept = append(kept, s)
		}
	}
	return kept
}

func (b *canonicalBuilder) writeArticlesFile(ctx context.Context, path string, format datamapping.OutputFormat) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close %s: %w", path, closeErr)
		}
	}()
	buffered := bufio.NewWriterSize(f, 1<<20)
	writer := newCanonicalWriter(buffered, format)

	if err := b.writeTo(ctx, writer); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish %s: %w", path, err)
	}
	if err := buffered.Flush(); err != nil {
		return fmt.Errorf("flush %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", path, err)
	}
	return nil
}

func newCanonicalWriter(w io.Writer, format datamapping.OutputFormat) CanonicalWriter {
	if format == datamapping.OutputJSONL {
		return NewJsonlCanonicalWriter(w)
	}
	return NewParquetCanonicalWriter(w)
}

// canonicalBuilder maps the records of a raw package onto articles and counts
// them into report. Of records that share an id, the first in source order is
// kept.
type canonicalBuilder struct {
	raw    *datapackage.Opened
	mapper reader.Mapper
	seen   map[uuid.UUID]struct{}
	report datapackage.Report
}

const writeBatchSize = 1000

func (b *canonicalBuilder) writeTo(ctx context.Context, w CanonicalWriter) error {
	batch := make([]document.Article, 0, writeBatchSize)
	for _, resource := range b.raw.Package.Resources {
		err := b.eachArticle(ctx, resource, func(article document.Article) error {
			batch = append(batch, article)
			if len(batch) < writeBatchSize {
				return nil
			}
			err := w.Write(batch)
			batch = batch[:0]
			return err
		})
		if err != nil {
			return err
		}
	}
	if err := w.Write(batch); err != nil {
		return fmt.Errorf("write articles: %w", err)
	}
	if b.report.Written == 0 {
		return fmt.Errorf("no article survived mapping out of %d records read: %v", b.report.Read, b.report.Dropped)
	}
	return nil
}

func (b *canonicalBuilder) eachArticle(ctx context.Context, resource datapackage.Resource, emit func(document.Article) error) error {
	file, err := b.raw.Resource(ctx, resource.Name)
	if err != nil {
		return fmt.Errorf("open raw resource %s: %w", resource.Name, err)
	}
	defer file.Close()
	records, err := recordReaderFor(resource, file)
	if err != nil {
		return err
	}
	for record, err := range records.Records() {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		b.report.Read++
		var malformed *reader.MalformedRecordError
		if errors.As(err, &malformed) {
			b.report.Dropped[dropMalformed]++
			continue
		}
		if err != nil {
			return fmt.Errorf("read raw resource %s: %w", resource.Name, err)
		}
		article, err := b.mapper.Map(record)
		var drop *reader.MappingDropError
		if errors.As(err, &drop) {
			slog.Debug("dropped record", "resource", resource.Name, "error", err)
			b.report.Dropped[string(drop.Reason)+"/"+drop.Target]++
			continue
		}
		if err != nil {
			return fmt.Errorf("map record from %s: %w", resource.Name, err)
		}
		if _, duplicate := b.seen[article.ID]; duplicate {
			b.report.Deduped++
			continue
		}
		b.seen[article.ID] = struct{}{}
		if err := emit(article); err != nil {
			return fmt.Errorf("write articles: %w", err)
		}
		b.report.Written++
	}
	return nil
}

const dropMalformed = "malformed_record"

func recordReaderFor(resource datapackage.Resource, file *datapackage.ResourceFile) (reader.RecordReader, error) {
	switch resource.Format {
	case "csv":
		return reader.NewCSVReader(file), nil
	case "jsonl":
		return reader.NewJSONLReader(file), nil
	case "parquet":
		parquet, err := reader.NewParquetReader(file, file.Size())
		if err != nil {
			return nil, fmt.Errorf("open raw resource %s as parquet: %w", resource.Name, err)
		}
		return parquet, nil
	default:
		return nil, fmt.Errorf("raw resource %s has format %q, want csv, jsonl or parquet", resource.Name, resource.Format)
	}
}
