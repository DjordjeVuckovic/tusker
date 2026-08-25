package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testMapping = `
kind: DataMapper
version: v1
metadata:
  name: "Test"
dataset: test_corpus
dateFormat: "2006-01-02T15:04:05Z"
fieldMappings:
  - source: "title"
    sourceType: "string"
    target: "Title"
    required: true
  - source: "body"
    sourceType: "string"
    target: "Content"
    required: false
`

func writeFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func runFixturePreprocess(t *testing.T, cfg preprocessConfig) PreprocessReport {
	t.Helper()
	require.NoError(t, runPreprocess(t.Context(), cfg))

	base := filepath.Base(cfg.OutputPath)
	reportPath := filepath.Join(filepath.Dir(cfg.OutputPath),
		base[:len(base)-len(filepath.Ext(base))]+"-report.json")

	raw, err := os.ReadFile(reportPath)
	require.NoError(t, err)

	var report PreprocessReport
	require.NoError(t, json.Unmarshal(raw, &report))
	return report
}

func fixtureConfig(t *testing.T, rows string) preprocessConfig {
	t.Helper()
	dir := t.TempDir()
	writeReport := true
	return preprocessConfig{
		InputPath:   writeFixture(t, dir, "in.jsonl", rows),
		OutputPath:  filepath.Join(dir, "canonical.jsonl"),
		MappingPath: writeFixture(t, dir, "mapping.yaml", testMapping),
		Workers:     2,
		WriteReport: &writeReport,
	}
}

func TestPreprocessReportChecksumMatchesOutput(t *testing.T) {
	cfg := fixtureConfig(t, `{"title":"one","body":"a"}
{"title":"two","body":"b"}
`)

	report := runFixturePreprocess(t, cfg)

	written, err := os.ReadFile(cfg.OutputPath)
	require.NoError(t, err)
	sum := sha256.Sum256(written)

	assert.Equal(t, hex.EncodeToString(sum[:]), report.SHA256)
}

func TestPreprocessCorpusId(t *testing.T) {
	tests := []struct {
		name     string
		override string
		want     string
	}{
		{name: "defaults to the mapping dataset", want: "test_corpus"},
		{name: "explicit id wins", override: "cc-news-v1", want: "cc-news-v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := fixtureConfig(t, `{"title":"one","body":"a"}
`)
			cfg.CorpusId = tt.override

			assert.Equal(t, tt.want, runFixturePreprocess(t, cfg).CorpusId)
		})
	}
}
