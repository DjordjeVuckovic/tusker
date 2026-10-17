package reader_test

import (
	"strings"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/ingest/reader"
	"github.com/DjordjeVuckovic/tusker/pkg/apis/datamapping"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validMapping = `kind: DataMapper
version: v1
metadata:
  name: Test
dataset: test
fieldMappings:
  - source: title
    target: Title
`

func loadMapping(doc string) (*datamapping.DataMapper, error) {
	return reader.NewYAMLConfigLoader(strings.NewReader(doc)).Load(true)
}

func TestLoadMapping_AcceptsAValidMapping(t *testing.T) {
	cfg, err := loadMapping(validMapping)
	require.NoError(t, err)
	assert.Equal(t, "test", cfg.Dataset)
}

func TestLoadMapping_RejectsAnUnknownKeyByName(t *testing.T) {
	tests := []struct {
		name    string
		doc     string
		wantKey string
	}{
		{
			name:    "top level",
			doc:     validMapping + "idStrategey:\n  kind: uuidv5\n  source: url\n",
			wantKey: "idStrategey",
		},
		{
			name:    "inside idStrategy",
			doc:     validMapping + "idStrategy:\n  kidn: uuidv5\n",
			wantKey: "kidn",
		},
		{
			name:    "inside a field mapping",
			doc:     strings.Replace(validMapping, "    target: Title\n", "    target: Title\n    requried: true\n", 1),
			wantKey: "requried",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadMapping(tt.doc)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantKey)
		})
	}
}
