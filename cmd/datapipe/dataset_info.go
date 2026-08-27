package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/DjordjeVuckovic/tusker/internal/cli"
	"github.com/DjordjeVuckovic/tusker/internal/ingest/reader"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/storage/factory"
)

// rowCounter is implemented by readers whose format records its row count in
// metadata. CSV and JSONL do not: counting would mean a second full pass over
// the file to print a number the summary reports anyway.
type rowCounter interface {
	NumRows() int64
}

// datasetFields describes the input a command is about to read. reader may be
// nil when the command has not opened one.
func datasetFields(path string, source reader.RawParallelReader) []cli.Field {
	fields := []cli.Field{
		{Label: "source", Value: path},
		{Label: "format", Value: strings.TrimPrefix(string(fileExt(path)), ".")},
	}
	if info, err := os.Stat(path); err == nil {
		fields = append(fields, cli.ByteField("size", info.Size()))
	}
	if counter, ok := source.(rowCounter); ok {
		fields = append(fields, cli.Field{Label: "rows", Value: strconv.FormatInt(counter.NumRows(), 10)})
	}
	return fields
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

var _ rowCounter = (*reader.ParquetReader)(nil)

// storageTarget names the backend without going near the connection string. A
// DSN carries the password, and the safest redaction is not reading it at all;
// the index name is the part of the target you can actually get wrong.
func storageTarget(cfg factory.StorageConfig) string {
	if cfg.Type == storage.ES && cfg.Es != nil {
		return fmt.Sprintf("elasticsearch index=%s", cfg.Es.IndexName)
	}
	return string(cfg.Type)
}
