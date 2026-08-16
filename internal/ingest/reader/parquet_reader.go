package reader

import (
	"context"
	"io"
	"strings"
	"sync"

	"github.com/parquet-go/parquet-go"
)

const parquetRowBatchSize = 256

var (
	_ Reader            = (*ParquetReader)(nil)
	_ RawParallelReader = (*ParquetReader)(nil)
)

type ParquetReader struct {
	file    *parquet.File
	columns []string
}

func NewParquetReader(r io.ReaderAt, size int64) (*ParquetReader, error) {
	f, err := parquet.OpenFile(r, size)
	if err != nil {
		return nil, err
	}

	paths := f.Schema().Columns()
	cols := make([]string, 0, len(paths))
	for _, path := range paths {
		cols = append(cols, strings.Join(path, "."))
	}

	return &ParquetReader{
		file:    f,
		columns: cols,
	}, nil
}

func (p *ParquetReader) Read() ([]map[string]string, error) {
	var records []map[string]string
	for _, rg := range p.file.RowGroups() {
		err := p.eachRecord(rg, func(record map[string]string) error {
			records = append(records, record)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return records, nil
}

func (p *ParquetReader) ReadParallel(ctx context.Context, workerCount int) (<-chan ParallelReaderResult, error) {
	rowGroups := p.file.RowGroups()
	out := make(chan ParallelReaderResult)

	if workerCount < 1 {
		workerCount = 1
	}
	if workerCount > len(rowGroups) {
		workerCount = len(rowGroups)
	}

	jobs := make(chan int, len(rowGroups))
	for i := range rowGroups {
		jobs <- i
	}
	close(jobs)

	var wg sync.WaitGroup
	wg.Add(workerCount)
	for w := 0; w < workerCount; w++ {
		go func() {
			defer wg.Done()
			for i := range jobs {
				err := p.eachRecord(rowGroups[i], func(record map[string]string) error {
					select {
					case out <- ParallelReaderResult{Record: record}:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
				if err == nil {
					continue
				}
				if ctx.Err() != nil {
					return
				}
				select {
				case out <- ParallelReaderResult{Err: err}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out, nil
}

// eachRecord decodes a row group in batches, converting every row to a record
// before the next ReadRows call: byte array values point into page buffers the
// reader recycles as it advances.
func (p *ParquetReader) eachRecord(rg parquet.RowGroup, fn func(map[string]string) error) (err error) {
	rows := rg.Rows()
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	buf := make([]parquet.Row, parquetRowBatchSize)
	for {
		n, readErr := rows.ReadRows(buf)

		// ReadRows may return rows together with io.EOF, so the batch is
		// consumed before the error is inspected.
		for _, row := range buf[:n] {
			if fnErr := fn(rowToRecord(row, p.columns)); fnErr != nil {
				return fnErr
			}
		}

		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
		if n == 0 {
			return nil
		}
	}
}

// rowToRecord flattens a parquet row onto its column names
func rowToRecord(row parquet.Row, columns []string) map[string]string {
	record := make(map[string]string, len(columns))
	for _, v := range row {
		col := v.Column()
		if col < 0 || col >= len(columns) || v.IsNull() {
			continue
		}

		name := columns[col]
		if prev, seen := record[name]; seen {
			record[name] = prev + "," + v.String()
			continue
		}
		record[name] = v.String()
	}
	return record
}
