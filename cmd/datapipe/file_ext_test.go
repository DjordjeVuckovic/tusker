package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileExt(t *testing.T) {
	tests := []struct {
		name string
		path string
		want FileExt
	}{
		{name: "lowercases the extension", path: "/datasets/DATASET.CSV", want: ExtCSV},
		{name: "reads the file, not the directory", path: "canonical.parquet/part.jsonl", want: ExtJSONL},
		{name: "path without an extension", path: "datasets/canonical", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, fileExt(tt.path))
		})
	}
}

func TestValidateFileExt(t *testing.T) {
	supported := map[FileExt]struct{}{ExtJSONL: {}, ExtParquet: {}}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "supported extension", path: "canonical.jsonl"},
		{name: "supported extension in upper case", path: "canonical.PARQUET"},
		{name: "extension outside the set", path: "canonical.csv", wantErr: true},
		{name: "no extension at all", path: "canonical", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFileExt(tt.path, supported)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorContains(t, err, string(fileExt(tt.path)))
		})
	}
}
