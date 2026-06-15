package ingest

import (
	"encoding/json"
	"io"

	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/parquet-go/parquet-go"
)

type CanonicalWriter interface {
	Write(ar []document.CanonicalArticle) error
	Close() error
}

type JsonlCanonicalWriter struct {
	enc *json.Encoder
}

func NewJsonlCanonicalWriter(writer io.Writer) *JsonlCanonicalWriter {
	return &JsonlCanonicalWriter{
		enc: json.NewEncoder(writer),
	}
}

func (j *JsonlCanonicalWriter) Write(ars []document.CanonicalArticle) error {
	for _, ar := range ars {
		if err := j.enc.Encode(ar); err != nil {
			return err
		}
	}
	return nil
}

func (j *JsonlCanonicalWriter) Close() error {
	return nil
}

type ParquetCanonicalWriter struct {
	w *parquet.GenericWriter[document.CanonicalArticle]
}

func NewParquetCanonicalWriter(writer io.Writer) *ParquetCanonicalWriter {
	return &ParquetCanonicalWriter{
		w: parquet.NewGenericWriter[document.CanonicalArticle](writer),
	}
}

func (p *ParquetCanonicalWriter) Write(ar []document.CanonicalArticle) error {
	_, err := p.w.Write(ar)
	if err != nil {
		return err
	}
	return nil
}

func (p *ParquetCanonicalWriter) Close() error {
	return p.w.Close()
}
