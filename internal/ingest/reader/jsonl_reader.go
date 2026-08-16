package reader

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

type JSONLReader struct {
	reader io.Reader
}

func NewJSONLReader(reader io.Reader) *JSONLReader {
	return &JSONLReader{reader: reader}
}

const BufferSize = 10 * 1024 * 1024 // 10 MB

func (jr *JSONLReader) Read() ([]map[string]string, error) {
	scanner := bufio.NewScanner(jr.reader)
	scanner.Buffer(make([]byte, BufferSize), BufferSize)

	var records []map[string]string
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		record, err := decodeJSONLine(line)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, scanner.Err()
}

func (jr *JSONLReader) ReadParallel(ctx context.Context, workerCount int) (<-chan ParallelReaderResult, error) {
	out := make(chan ParallelReaderResult)

	jobs := make(chan []byte, workerCount*2)
	var wg sync.WaitGroup

	wg.Add(workerCount)
	for w := 0; w < workerCount; w++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case line, ok := <-jobs:
					if !ok {
						return
					}
					record, err := decodeJSONLine(line)
					select {
					case out <- ParallelReaderResult{Record: record, Err: err}:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		scanner := bufio.NewScanner(jr.reader)
		scanner.Buffer(make([]byte, 10*1024*1024), 10*1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			cp := make([]byte, len(line))
			copy(cp, line)
			select {
			case jobs <- cp:
			case <-ctx.Done():
				slog.Info("Context cancelled, stopping JSONL read...")
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case out <- ParallelReaderResult{Err: err}:
			case <-ctx.Done():
			}
		}
	}()

	go func() {
		wg.Wait()
		close(out)
	}()

	return out, nil
}

func decodeJSONLine(line []byte) (map[string]string, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, fmt.Errorf("failed to decode JSON line: %w", err)
	}
	record := make(map[string]string, len(raw))
	for k, v := range raw {
		switch s := v.(type) {
		case string:
			record[k] = s
		case nil:
			slog.Info("skipping null value")
			// skip null values
		default:
			record[k] = fmt.Sprintf("%v", v)
		}
	}
	return record, nil
}
