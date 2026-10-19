package reader

import (
	"context"
	"encoding/csv"
	"errors"
	"io"
	"iter"
	"log/slog"
	"sync"
)

type CSVReader struct {
	reader io.Reader
}

func NewCSVReader(reader io.Reader) *CSVReader {
	return &CSVReader{
		reader: reader,
	}
}

func (cr *CSVReader) Read() ([]map[string]string, error) {
	// Create a new CSV reader
	csvReader := csv.NewReader(cr.reader)

	headers, err := csvReader.Read()
	if err != nil {
		return nil, err
	}

	var records []map[string]string
	for {
		row, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		record := make(map[string]string)
		for i, h := range headers {
			record[h] = row[i]
		}
		records = append(records, record)
	}

	return records, nil
}

func (cr *CSVReader) Records() iter.Seq2[map[string]string, error] {
	return func(yield func(map[string]string, error) bool) {
		csvReader := csv.NewReader(cr.reader)
		headers, err := csvReader.Read()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			yield(nil, err)
			return
		}
		for {
			row, err := csvReader.Read()
			if errors.Is(err, io.EOF) {
				return
			}
			var parseErr *csv.ParseError
			if errors.As(err, &parseErr) {
				if !yield(nil, &MalformedRecordError{Line: int64(parseErr.StartLine), Err: err}) {
					return
				}
				continue
			}
			if err != nil {
				yield(nil, err)
				return
			}
			record := make(map[string]string, len(headers))
			for i, h := range headers {
				record[h] = row[i]
			}
			if !yield(record, nil) {
				return
			}
		}
	}
}

func (cr *CSVReader) ReadParallel(ctx context.Context, workerCount int) (<-chan ParallelReaderResult, error) {
	out := make(chan ParallelReaderResult) // The output channel (streaming results)
	csvReader := csv.NewReader(cr.reader)

	headers, err := csvReader.Read()
	if err != nil {
		return nil, err
	}

	// Buffered job channel to allow decoupling read/processing
	jobs := make(chan []string, workerCount*2)
	var wg sync.WaitGroup

	// Start worker goroutines
	wg.Add(workerCount)
	for w := 0; w < workerCount; w++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case row, ok := <-jobs:
					if !ok {
						return
					}
					if len(row) != len(headers) {
						select {
						case out <- ParallelReaderResult{Err: io.ErrUnexpectedEOF}:
						case <-ctx.Done():
						}
						continue
					}
					record := make(map[string]string, len(headers))
					for i, h := range headers {
						record[h] = row[i]
					}
					select {
					case out <- ParallelReaderResult{Record: record}:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}

	// Feed jobs into the channel in a separate goroutine
	go func() {
		defer close(jobs)

		for {
			row, err := csvReader.Read()
			if err == io.EOF {
				return
			}
			if err != nil {
				select {
				case out <- ParallelReaderResult{Err: err}:
				case <-ctx.Done():
					slog.Info("Context cancelled, stopping CSV read...")
				}
				slog.Error("Error reading CSV row", "error", err)
				continue
			}
			jobs <- row
		}
	}()

	go func() {
		wg.Wait()
		close(out)
	}()

	return out, nil
}
