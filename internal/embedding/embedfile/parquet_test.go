package embedfile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/parquet-go/parquet-go"
)

// fixtureRow is intentionally independent of parquetRow and forces the 3-level
// LIST encoding that pyarrow produces. If parquetRow ever loses its `,list` tag,
// reading this fixture decodes to nil and the tests fail (regression guard).
type fixtureRow struct {
	ID        string    `parquet:"id"`
	Embedding []float32 `parquet:"embedding,list"`
}

func writeFixture(t *testing.T, rows []fixtureRow, meta map[string]string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "embeddings.parquet")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	defer f.Close()

	kv := make([]parquet.WriterOption, 0, len(meta))
	for k, v := range meta {
		kv = append(kv, parquet.KeyValueMetadata(k, v))
	}

	w := parquet.NewGenericWriter[fixtureRow](f, kv...)
	if _, err := w.Write(rows); err != nil {
		t.Fatalf("write rows: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return path
}

func TestReadRecordsAndMeta(t *testing.T) {
	rows := []fixtureRow{
		{ID: "11111111-1111-1111-1111-111111111111", Embedding: []float32{0.1, 0.2, 0.3}},
		{ID: "22222222-2222-2222-2222-222222222222", Embedding: []float32{0.4, 0.5, 0.6}},
		{ID: "33333333-3333-3333-3333-333333333333", Embedding: []float32{0.7, 0.8, 0.9}},
	}
	path := writeFixture(t, rows, map[string]string{
		"model":     "qwen3-embedding:0.6b",
		"dim":       "3",
		"pooling":   "last_token",
		"row_count": "3",
	})

	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	meta := r.Meta()
	if meta.Model != "qwen3-embedding:0.6b" {
		t.Errorf("model = %q, want qwen3-embedding:0.6b", meta.Model)
	}
	if meta.Dim != 3 {
		t.Errorf("dim = %d, want 3", meta.Dim)
	}
	if meta.RowCount != 3 {
		t.Errorf("row_count = %d, want 3", meta.RowCount)
	}

	var got []Record
	buf := make([]Record, 2) // smaller than total to exercise batching
	for {
		n, err := r.Read(buf)
		got = append(got, buf[:n]...)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
	}

	if len(got) != len(rows) {
		t.Fatalf("read %d records, want %d", len(got), len(rows))
	}
	for i, rec := range got {
		if rec.ID != rows[i].ID {
			t.Errorf("record %d id = %q, want %q", i, rec.ID, rows[i].ID)
		}
		if len(rec.Embedding) != len(rows[i].Embedding) {
			t.Fatalf("record %d dim = %d, want %d", i, len(rec.Embedding), len(rows[i].Embedding))
		}
		for j, v := range rec.Embedding {
			if v != rows[i].Embedding[j] {
				t.Errorf("record %d[%d] = %v, want %v", i, j, v, rows[i].Embedding[j])
			}
		}
	}
}

// The Colab notebook stamps key-value metadata; other writers do not, and the
// file still has to describe itself.
func TestMetaWithoutKeyValueBlock(t *testing.T) {
	rows := []fixtureRow{
		{ID: "11111111-1111-1111-1111-111111111111", Embedding: []float32{0.1, 0.2, 0.3, 0.4}},
		{ID: "22222222-2222-2222-2222-222222222222", Embedding: []float32{0.5, 0.6, 0.7, 0.8}},
	}
	path := writeFixture(t, rows, nil)

	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	meta := r.Meta()
	if meta.RowCount != 2 {
		t.Errorf("row_count = %d, want 2 — the footer carries it even with no metadata block", meta.RowCount)
	}
	if meta.Dim != 4 {
		t.Errorf("dim = %d, want 4 — derived from the first row", meta.Dim)
	}
	if meta.Model != "" {
		t.Errorf("model = %q, want empty; only the file can claim a model", meta.Model)
	}
}

// Deriving the dimension must not consume the row it read.
func TestDimPeekDoesNotSwallowTheFirstRow(t *testing.T) {
	rows := []fixtureRow{
		{ID: "11111111-1111-1111-1111-111111111111", Embedding: []float32{0.1, 0.2}},
		{ID: "22222222-2222-2222-2222-222222222222", Embedding: []float32{0.3, 0.4}},
	}
	r, err := Open(writeFixture(t, rows, nil))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	var got []Record
	buf := make([]Record, 8)
	for {
		n, err := r.Read(buf)
		got = append(got, buf[:n]...)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
	}
	if len(got) != 2 {
		t.Fatalf("read %d records, want 2", len(got))
	}
	if got[0].ID != rows[0].ID {
		t.Errorf("first record = %q, want %q", got[0].ID, rows[0].ID)
	}
}

func TestEmptyFileHasNoDimensionAndNoRows(t *testing.T) {
	r, err := Open(writeFixture(t, nil, nil))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	if meta := r.Meta(); meta.RowCount != 0 || meta.Dim != 0 {
		t.Errorf("meta = %+v, want zero rows and zero dim", meta)
	}
}
