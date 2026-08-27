// Package embedfile reads precomputed document embeddings produced offline
// (see scripts/embed_qwen3.ipynb) from a Parquet file with columns
// `id` (string, article UUID) and `embedding` (list<float32>), plus file-level
// metadata carrying the model name and vector dimension.
package embedfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/parquet-go/parquet-go"
)

// Meta holds the file-level provenance written by the embedding notebook.
type Meta struct {
	Model      string
	Dim        int
	Pooling    string
	Normalized string
	RowCount   int
	CreatedAt  string
}

// Record is a single decoded embedding row.
type Record struct {
	ID        string
	Embedding []float32
}

type parquetRow struct {
	ID string `parquet:"id"`
	// `,list` matches the 3-level LIST encoding pyarrow writes
	// (group embedding (LIST) { repeated group list { float element } }).
	// Without it parquet-go expects a flat repeated field and decodes to nil.
	Embedding []float32 `parquet:"embedding,list"`
}

// Reader streams Records from a Parquet embeddings file.
type Reader struct {
	file *os.File
	pr   *parquet.GenericReader[parquetRow]
	meta Meta
}

// Open opens the Parquet file at path and reads its metadata block.
func Open(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open embeddings file: %w", err)
	}

	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("stat embeddings file: %w", err)
	}

	pf, err := parquet.OpenFile(f, stat.Size())
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("parse parquet file: %w", err)
	}

	pr := parquet.NewGenericReader[parquetRow](f)
	meta := metaFrom(pf)
	if meta.Dim == 0 {
		dim, err := peekDim(pr)
		if err != nil {
			pr.Close()
			f.Close()
			return nil, fmt.Errorf("read first embedding: %w", err)
		}
		meta.Dim = dim
	}

	return &Reader{file: f, pr: pr, meta: meta}, nil
}

// peekDim reads the width off the first row for files that do not declare it,
// then rewinds. Without it an undeclared dimension reads as 0 and the caller
// has to assume one.
func peekDim(pr *parquet.GenericReader[parquetRow]) (int, error) {
	rows := make([]parquetRow, 1)
	n, err := pr.Read(rows)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, err
	}
	if err := pr.SeekToRow(0); err != nil {
		return 0, fmt.Errorf("rewind after reading first row: %w", err)
	}
	if n == 0 {
		return 0, nil
	}
	return len(rows[0].Embedding), nil
}

// Meta returns the file-level metadata.
func (r *Reader) Meta() Meta { return r.meta }

// Read fills buf with up to len(buf) records, returning the count read.
// It returns io.EOF when the file is exhausted (possibly with n > 0).
func (r *Reader) Read(buf []Record) (int, error) {
	// Fresh slice per call: the decoded float slices are handed out to callers
	// (and retained in embedding.Vec), so they must not be reused across batches.
	rows := make([]parquetRow, len(buf))
	n, err := r.pr.Read(rows)
	for i := 0; i < n; i++ {
		buf[i] = Record{ID: rows[i].ID, Embedding: rows[i].Embedding}
	}
	return n, err
}

func (r *Reader) Close() error {
	r.pr.Close()
	return r.file.Close()
}

func metaFrom(pf *parquet.File) Meta {
	lookup := func(key string) string {
		v, _ := pf.Lookup(key)
		return v
	}
	dim, _ := strconv.Atoi(lookup("dim"))
	return Meta{
		Model:      lookup("model"),
		Dim:        dim,
		Pooling:    lookup("pooling"),
		Normalized: lookup("normalized"),
		// The footer always carries the row count; the key-value block is
		// optional and absent from files the notebook did not stamp.
		RowCount:  int(pf.NumRows()),
		CreatedAt: lookup("created_at"),
	}
}
