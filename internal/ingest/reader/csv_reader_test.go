package reader

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCSVReader_Read(t *testing.T) {
	csvData := `id,title,author,published
1,Go Concurrency,John Doe,2023-10-01
2,Understanding Interfaces,Jane Smith,2023-10-02`

	reader := NewCSVReader(strings.NewReader(csvData))

	records, err := reader.Read()
	require.NoError(t, err)
	require.Len(t, records, 2)

	assert.Equal(t, map[string]string{
		"id":        "1",
		"title":     "Go Concurrency",
		"author":    "John Doe",
		"published": "2023-10-01",
	}, records[0])

	assert.Equal(t, map[string]string{
		"id":        "2",
		"title":     "Understanding Interfaces",
		"author":    "Jane Smith",
		"published": "2023-10-02",
	}, records[1])
}

func TestCSVReader_ReadParallel(t *testing.T) {
	csvData := `id,title,author,published
1,Go Concurrency,John Doe,2023-10-01
2,Understanding Interfaces,Jane Smith,2023-10-02
3,Intro to Channels,Alice,2023-10-03`

	ctx := t.Context()
	reader := NewCSVReader(strings.NewReader(csvData))

	resultsChan, err := reader.ReadParallel(ctx, 2)
	require.NoError(t, err)

	var results []map[string]string
	for res := range resultsChan {
		require.NoError(t, res.Err)
		results = append(results, res.Record)
	}

	assert.Len(t, results, 3)

	// Optionally check one by one (order is preserved in your current implementation)
	assert.Contains(t, results, map[string]string{
		"id":        "1",
		"title":     "Go Concurrency",
		"author":    "John Doe",
		"published": "2023-10-01",
	})
	assert.Contains(t, results, map[string]string{
		"id":        "2",
		"title":     "Understanding Interfaces",
		"author":    "Jane Smith",
		"published": "2023-10-02",
	})
	assert.Contains(t, results, map[string]string{
		"id":        "3",
		"title":     "Intro to Channels",
		"author":    "Alice",
		"published": "2023-10-03",
	})
}

func TestCSVReader_ReadParallel_CancelEarly(t *testing.T) {
	csvData := `id,title,author,published
1,Go Concurrency,John Doe,2023-10-01
2,Understanding Interfaces,Jane Smith,2023-10-02
3,Intro to Channels,Alice,2023-10-03`

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reader := NewCSVReader(strings.NewReader(csvData))

	resultsChan, err := reader.ReadParallel(ctx, 2)
	require.NoError(t, err)

	var results []map[string]string
	for res := range resultsChan {
		require.NoError(t, res.Err)
		results = append(results, res.Record)
		if len(results) == 1 {
			cancel() // simulate early exit
			break
		}
	}

	assert.Len(t, results, 1)
}

func TestCSVReader_ReadParallel_MalformedRow(t *testing.T) {
	csvData := `id,title,author
1,Go Concurrency,John Doe
2,OnlyTitle
3,Interfaces in Go,Alice`

	ctx := t.Context()
	reader := NewCSVReader(strings.NewReader(csvData))

	resultsChan, err := reader.ReadParallel(ctx, 2)
	require.NoError(t, err)

	var validResults []map[string]string
	var errorCount int

	for res := range resultsChan {
		if res.Err != nil {
			errorCount++
			continue
		}
		validResults = append(validResults, res.Record)
	}

	assert.Equal(t, 2, len(validResults))
	assert.Equal(t, 1, errorCount)
	assert.Contains(t, validResults[0], "id")
}
