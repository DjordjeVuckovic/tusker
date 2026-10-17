// Package queryvectors serves a bench track's precomputed query vectors from
// its query-vectors data package, so a run needs no live query embedder.
package queryvectors

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/DjordjeVuckovic/tusker/internal/datapackage"
	"github.com/DjordjeVuckovic/tusker/internal/storage/pg"
	"github.com/google/uuid"
	"github.com/parquet-go/parquet-go"
)

// Options locates a query-vectors package and what it must agree with.
type Options struct {
	// Package is the package directory or descriptor location.
	Package string
	// Suite is the suite.yaml the bench runs.
	Suite string
	// Model is the embedding tag the spec declares as embedding_model.
	Model string
	// Postgres serves document vectors; nil leaves DocVectors refusing.
	Postgres *pg.ConnectionPool
}

// Store is a storage.VectorStore whose query vectors come from a package and
// whose document vectors come from Postgres, filtered by the package's tag.
type Store struct {
	location string
	tag      string
	vectors  map[string][]float32
	docs     *pg.VectorStore
}

// UnknownQueryError reports a query text the package holds no vector for.
type UnknownQueryError struct {
	Package string
	Text    string
}

func (e *UnknownQueryError) Error() string {
	return fmt.Sprintf("query-vectors package %s has no vector for query text %q", e.Package, e.Text)
}

// SuiteMismatchError reports a package embedded from another version of the
// suite than the one the bench runs.
type SuiteMismatchError struct {
	Package     string
	PackageHash string
	Suite       string
	SuiteHash   string
}

func (e *SuiteMismatchError) Error() string {
	return fmt.Sprintf("query-vectors package %s was embedded from a suite hashing %s, but %s now hashes %s: re-embed the queries",
		e.Package, e.PackageHash, e.Suite, e.SuiteHash)
}

type row struct {
	QueryID string `parquet:"query_id"`
	Text    string `parquet:"text"`
	// `,list` matches the 3-level LIST encoding pyarrow writes.
	Embedding []float32 `parquet:"embedding,list"`
}

// Open reads and verifies the package and loads its vectors.
func Open(ctx context.Context, opts Options) (*Store, error) {
	opened, err := datapackage.Open(ctx, opts.Package)
	if err != nil {
		return nil, fmt.Errorf("open query-vectors package: %w", err)
	}
	pkg := opened.Package
	if pkg.Embedding == nil {
		return nil, fmt.Errorf("query-vectors package %s records no embedding", opened.Location)
	}
	if err := checkSuite(opened, opts.Suite); err != nil {
		return nil, err
	}
	if err := checkEmbedding(opened, opts.Model); err != nil {
		return nil, err
	}
	vectors, err := readVectors(ctx, opened)
	if err != nil {
		return nil, err
	}
	store := &Store{location: opened.Location, tag: pkg.Embedding.Tag, vectors: vectors}
	if opts.Postgres != nil {
		store.docs = pg.NewVectorStore(nil, opts.Postgres, store.tag)
	}
	return store, nil
}

func checkSuite(opened *datapackage.Opened, suitePath string) error {
	if opened.Package.Suite == nil {
		return fmt.Errorf("query-vectors package %s records no suite", opened.Location)
	}
	current, err := fileHash(suitePath)
	if err != nil {
		return fmt.Errorf("hash suite %s: %w", suitePath, err)
	}
	if current != opened.Package.Suite.Hash {
		return &SuiteMismatchError{
			Package:     opened.Location,
			PackageHash: opened.Package.Suite.Hash,
			Suite:       suitePath,
			SuiteHash:   current,
		}
	}
	return nil
}

// QueryRecipe is the only query recipe the bench embeds with.
const QueryRecipe = "query-instruct-v1"

func checkEmbedding(opened *datapackage.Opened, declared string) error {
	embedding := opened.Package.Embedding
	if declared == "" {
		return fmt.Errorf("query-vectors package %s is tagged %s, but the spec declares no embedding_model to check it against",
			opened.Location, embedding.Tag)
	}
	if embedding.Tag != declared {
		return fmt.Errorf("query-vectors package %s is tagged %s, but the spec declares embedding_model %s",
			opened.Location, embedding.Tag, declared)
	}
	if embedding.Recipe != QueryRecipe {
		return fmt.Errorf("query-vectors package %s was embedded with recipe %q, want %q",
			opened.Location, embedding.Recipe, QueryRecipe)
	}
	return nil
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func readVectors(ctx context.Context, opened *datapackage.Opened) (map[string][]float32, error) {
	resource, err := opened.Resource(ctx, datapackage.QueryVectorsResource)
	if err != nil {
		return nil, fmt.Errorf("read query vectors of %s: %w", opened.Location, err)
	}
	defer resource.Close()
	file, err := parquet.OpenFile(resource, resource.Size())
	if err != nil {
		return nil, fmt.Errorf("parse query vectors of %s: %w", opened.Location, err)
	}
	reader := parquet.NewGenericReader[row](file)
	defer reader.Close()

	dim := opened.Package.Embedding.Dim

	vectors := make(map[string][]float32)
	for {
		batch := make([]row, 64)
		n, err := reader.Read(batch)
		for _, r := range batch[:n] {
			if len(r.Embedding) != dim {
				return nil, fmt.Errorf("query %s in %s has %d dimensions, the package declares %d",
					r.QueryID, opened.Location, len(r.Embedding), dim)
			}
			vectors[r.Text] = r.Embedding
		}
		if errors.Is(err, io.EOF) {
			return vectors, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read query vectors of %s: %w", opened.Location, err)
		}
	}
}

func (s *Store) QueryVector(_ context.Context, text string) ([]float32, error) {
	vector, ok := s.vectors[text]
	if !ok {
		return nil, &UnknownQueryError{Package: s.location, Text: text}
	}
	return vector, nil
}

func (s *Store) DocVectors(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]float32, error) {
	if s.docs == nil {
		return nil, errors.New("query-vectors store has no Postgres to read document vectors from")
	}
	return s.docs.DocVectors(ctx, ids)
}

func (s *Store) Model() string { return s.tag }
