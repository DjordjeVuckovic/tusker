package ingest

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

// canonicalVersionPrefix starts every canonical package version.
const canonicalVersionPrefix = "article-v2"

// articleIDNamespace is the namespace document.DeriveArticleID hashes under,
// recorded so a descriptor alone says how its ids were derived.
const articleIDNamespace = "697bf49f-ca14-4303-83eb-f9d05f7c7c42"

// PreprocessOptions names the raw package to map, the mapping that maps it and
// the folder that receives canonical/<version>.
type PreprocessOptions struct {
	Raw *datapackage.Opened
	// Mapping holds the mapping file's bytes; MappingPath is recorded in the
	// descriptor and must be relative.
	Mapping     []byte
	MappingPath string
	OutputRoot  string
	ProducedBy  datapackage.ProducedBy
}

// Preprocessed is the canonical package as its descriptor records it, and the
// folder that holds it.
type Preprocessed struct {
	Package datapackage.Package
	Dir     string
}

// Preprocess maps every record of the raw package, in the raw descriptor's
// resource order, onto an article and writes the canonical package to
// OutputRoot/<version>.
func Preprocess(ctx context.Context, opts PreprocessOptions) (Preprocessed, error) {
	if filepath.IsAbs(opts.MappingPath) {
		return Preprocessed{}, fmt.Errorf("mapping path %s must be relative, since it is recorded in a committed descriptor", opts.MappingPath)
	}
	mapping, err := reader.NewYAMLConfigLoader(bytes.NewReader(opts.Mapping)).Load(true)
	if err != nil {
		return Preprocessed{}, fmt.Errorf("load mapping %s: %w", opts.MappingPath, err)
	}
	mapper, err := reader.NewArticleMapper(mapping)
	if err != nil {
		return Preprocessed{}, fmt.Errorf("mapping %s: %w", opts.MappingPath, err)
	}
	format, err := mapping.Output.ResolvedFormat()
	if err != nil {
		return Preprocessed{}, err
	}
	lang, err := mapping.Constants.ResolvedLanguage()
	if err != nil {
		return Preprocessed{}, err
	}

	raw := opts.Raw.Package
	mappingHash := sha256Hash(opts.Mapping)
	pkg := datapackage.Package{
		Name:          canonicalName(raw.Name),
		Version:       canonicalVersion(mappingHash, raw),
		ArticleSchema: datapackage.ArticleSchema,
		Language:      lang,
		Mapping:       &datapackage.FileRef{Path: filepath.ToSlash(opts.MappingPath), Hash: mappingHash},
		IDStrategy:    idStrategyOf(mapping),
		ProducedBy:    &opts.ProducedBy,
		Raw:           rawRefOf(raw),
		Sources:       httpsSources(raw.Sources),
		Resources: []datapackage.Resource{{
			Name:   datapackage.ArticlesResource,
			Path:   datapackage.ArticlesResource + "." + string(format),
			Format: string(format),
		}},
	}

	return writePackage(ctx, opts.OutputRoot, pkg, corpusSource{raw: opts.Raw, mapper: mapper, seen: map[uuid.UUID]struct{}{}})
}

// writePackage builds the package in a temporary folder beside its version
// folder and renames it into place only once it is complete, so a failed or
// cancelled run leaves nothing behind. A version folder that already exists is
// never touched: matching content makes the run a no-op, and anything else is
// a *datapackage.DescriptorConflictError.
func writePackage(ctx context.Context, outputRoot string, pkg datapackage.Package, source corpusSource) (_ Preprocessed, err error) {
	if err := os.MkdirAll(outputRoot, 0o755); err != nil {
		return Preprocessed{}, fmt.Errorf("create canonical output folder: %w", err)
	}
	tmp, err := os.MkdirTemp(outputRoot, "."+pkg.Version+"-*")
	if err != nil {
		return Preprocessed{}, fmt.Errorf("create temporary package folder: %w", err)
	}
	defer func() {
		if tmp == "" {
			return
		}
		if removeErr := os.RemoveAll(tmp); removeErr != nil && err == nil {
			err = fmt.Errorf("remove temporary package folder: %w", removeErr)
		}
	}()

	report, err := writeArticlesFile(ctx, filepath.Join(tmp, pkg.Resources[0].Path), datamapping.OutputFormat(pkg.Resources[0].Format), source)
	if err != nil {
		return Preprocessed{}, err
	}
	pkg.Report = &report

	dir := filepath.Join(outputRoot, pkg.Version)
	exists, err := copyExistingDescriptor(dir, tmp)
	if err != nil {
		return Preprocessed{}, err
	}
	written, err := datapackage.Write(ctx, tmp, pkg)
	if err != nil {
		return Preprocessed{}, fmt.Errorf("canonical package %s: %w", dir, err)
	}
	if exists {
		return Preprocessed{Package: written, Dir: dir}, nil
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return Preprocessed{}, fmt.Errorf("open up temporary package folder: %w", err)
	}
	if err := os.Rename(tmp, dir); err != nil {
		return Preprocessed{}, fmt.Errorf("move canonical package into %s: %w", dir, err)
	}
	tmp = ""
	return Preprocessed{Package: written, Dir: dir}, nil
}

// copyExistingDescriptor puts the descriptor already at dir into tmp, so
// datapackage.Write compares the new resources against it rather than
// replacing it.
func copyExistingDescriptor(dir, tmp string) (bool, error) {
	existing, err := os.ReadFile(filepath.Join(dir, datapackage.DescriptorName))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read existing descriptor: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, datapackage.DescriptorName), existing, 0o644); err != nil {
		return false, fmt.Errorf("stage existing descriptor: %w", err)
	}
	return true, nil
}

func sha256Hash(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func canonicalVersion(mappingHash string, raw datapackage.Package) string {
	inputs := make([]string, 0, 2+len(raw.Resources))
	inputs = append(inputs, datapackage.ArticleSchema, mappingHash)
	for _, r := range raw.Resources {
		inputs = append(inputs, r.Hash)
	}
	return datapackage.Version(canonicalVersionPrefix, inputs...)
}

func canonicalName(rawName string) string {
	return strings.TrimSuffix(rawName, "-raw") + "-canonical"
}

// idStrategyOf records how ids were minted. A mapping that targets ID carries
// its own ids, which no strategy describes.
func idStrategyOf(mapping *datamapping.DataMapper) *datapackage.IDStrategy {
	for _, fm := range mapping.FieldMappings {
		if fm.Target == datamapping.TargetID {
			return nil
		}
	}
	kind, _ := mapping.IdStrategy.ResolvedKind()
	strategy := &datapackage.IDStrategy{Kind: kind, Source: mapping.IdStrategy.Source}
	if kind == datamapping.IdStrategyUuidV5 {
		strategy.Namespace = articleIDNamespace
	}
	return strategy
}

func rawRefOf(raw datapackage.Package) *datapackage.RawRef {
	ref := &datapackage.RawRef{Name: raw.Name, Version: raw.Version}
	for _, r := range raw.Resources {
		ref.Resources = append(ref.Resources, datapackage.ResourceRef{Name: r.Name, Hash: r.Hash})
	}
	return ref
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

func writeArticlesFile(ctx context.Context, path string, format datamapping.OutputFormat, source corpusSource) (report datapackage.Report, err error) {
	f, err := os.Create(path)
	if err != nil {
		return datapackage.Report{}, fmt.Errorf("create %s: %w", path, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close %s: %w", path, closeErr)
		}
	}()
	buffered := bufio.NewWriterSize(f, 1<<20)
	writer := newCanonicalWriter(buffered, format)

	report, err = source.writeTo(ctx, writer)
	if err != nil {
		return datapackage.Report{}, err
	}
	if err := writer.Close(); err != nil {
		return datapackage.Report{}, fmt.Errorf("finish %s: %w", path, err)
	}
	if err := buffered.Flush(); err != nil {
		return datapackage.Report{}, fmt.Errorf("flush %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		return datapackage.Report{}, fmt.Errorf("sync %s: %w", path, err)
	}
	return report, nil
}

func newCanonicalWriter(w io.Writer, format datamapping.OutputFormat) CanonicalWriter {
	if format == datamapping.OutputJSONL {
		return NewJsonlCanonicalWriter(w)
	}
	return NewParquetCanonicalWriter(w)
}

// corpusSource maps the records of a raw package onto articles. Of records
// that share an id, the first in source order is kept.
type corpusSource struct {
	raw    *datapackage.Opened
	mapper reader.Mapper
	seen   map[uuid.UUID]struct{}
}

const writeBatchSize = 1000

func (s corpusSource) writeTo(ctx context.Context, w CanonicalWriter) (datapackage.Report, error) {
	report := datapackage.Report{Dropped: map[string]int64{}}
	batch := make([]document.Article, 0, writeBatchSize)
	for _, resource := range s.raw.Package.Resources {
		err := s.eachArticle(ctx, resource, &report, func(article document.Article) error {
			batch = append(batch, article)
			if len(batch) < writeBatchSize {
				return nil
			}
			err := w.Write(batch)
			batch = batch[:0]
			return err
		})
		if err != nil {
			return datapackage.Report{}, err
		}
	}
	if err := w.Write(batch); err != nil {
		return datapackage.Report{}, err
	}
	if report.Written == 0 {
		return datapackage.Report{}, fmt.Errorf("no article survived mapping out of %d records read: %v", report.Read, report.Dropped)
	}
	return report, nil
}

func (s corpusSource) eachArticle(ctx context.Context, resource datapackage.Resource, report *datapackage.Report, emit func(document.Article) error) error {
	file, err := s.raw.Resource(ctx, resource.Name)
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
		report.Read++
		var malformed *reader.MalformedRecordError
		if errors.As(err, &malformed) {
			report.Dropped[dropMalformed]++
			continue
		}
		if err != nil {
			return fmt.Errorf("read raw resource %s: %w", resource.Name, err)
		}
		article, err := s.mapper.Map(record)
		var drop *reader.MappingDropError
		if errors.As(err, &drop) {
			slog.Debug("dropped record", "resource", resource.Name, "error", err)
			report.Dropped[string(drop.Reason)+"/"+drop.Target]++
			continue
		}
		if err != nil {
			return fmt.Errorf("map record from %s: %w", resource.Name, err)
		}
		if _, duplicate := s.seen[article.ID]; duplicate {
			report.Deduped++
			continue
		}
		s.seen[article.ID] = struct{}{}
		if err := emit(article); err != nil {
			return fmt.Errorf("write articles: %w", err)
		}
		report.Written++
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
