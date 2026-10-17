package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_EngineParamsAndAlias(t *testing.T) {
	yaml := validSpecHeader + `engines:
  pg-gin:
    type: postgres
    connection: "postgresql://localhost/test"
    params:
      rank_norm: "0"
  pg-gin-norm:
    type: postgres
    connection: "postgresql://localhost/test"
    queries_from: pg-gin
    params:
      rank_norm: "1"
jobs:
  - name: fts
    suite: suite.yaml
    engines: [pg-gin, pg-gin-norm]
`
	s, err := Parse([]byte(yaml))
	require.NoError(t, err)

	assert.Equal(t, "0", s.Engines["pg-gin"].Params["rank_norm"])
	assert.Equal(t, "1", s.Engines["pg-gin-norm"].Params["rank_norm"])
	assert.Equal(t, "pg-gin", s.Engines["pg-gin-norm"].QueriesFrom)
	assert.Empty(t, s.Engines["pg-gin"].QueriesFrom)
}

func TestParse_RejectsBrokenQueriesFrom(t *testing.T) {
	tests := []struct {
		name    string
		engines string
		jobs    string
		wantErr string
	}{
		{
			name: "points at itself",
			engines: `  pg-gin:
    type: postgres
    connection: "postgresql://localhost/test"
    queries_from: pg-gin
`,
			jobs:    "[pg-gin]",
			wantErr: "itself",
		},
		{
			name: "points at an undeclared engine",
			engines: `  pg-gin:
    type: postgres
    connection: "postgresql://localhost/test"
    queries_from: pg-typo
`,
			jobs:    "[pg-gin]",
			wantErr: "not a declared engine",
		},
		{
			name: "points at another alias",
			engines: `  arm-c:
    type: postgres
    connection: "postgresql://localhost/test"
    queries_from: arm-b
  arm-b:
    type: postgres
    connection: "postgresql://localhost/test"
    queries_from: arm-a
  arm-a:
    type: postgres
    connection: "postgresql://localhost/test"
`,
			jobs:    "[arm-a, arm-b, arm-c]",
			wantErr: "does not chain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml := validSpecHeader + "engines:\n" + tt.engines + `jobs:
  - name: fts
    suite: suite.yaml
    engines: ` + tt.jobs + "\n"
			_, err := Parse([]byte(yaml))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestParse_RejectsQueriesFromAcrossEngineTypes(t *testing.T) {
	tests := []struct {
		name    string
		engines string
	}{
		{
			name: "postgres reuses an elasticsearch block",
			engines: `  pg-arm:
    type: postgres
    connection: "postgresql://localhost/test"
    queries_from: es-arm
  es-arm:
    type: elasticsearch
    connection: "http://localhost:9200"
`,
		},
		{
			name: "elasticsearch reuses a postgres block",
			engines: `  pg-arm:
    type: postgres
    connection: "postgresql://localhost/test"
  es-arm:
    type: elasticsearch
    connection: "http://localhost:9200"
    queries_from: pg-arm
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml := validSpecHeader + "engines:\n" + tt.engines + `jobs:
  - name: fts
    suite: suite.yaml
    engines: [pg-arm, es-arm]
`
			_, err := Parse([]byte(yaml))
			require.Error(t, err)
			assert.ErrorContains(t, err, "pg-arm")
			assert.ErrorContains(t, err, "es-arm")
		})
	}
}

func TestBenchSpec_QueryBinding(t *testing.T) {
	s := &BenchSpec{Engines: map[string]Engine{
		"pg-gin": {Params: map[string]any{"rank_norm": "0"}},
		"pg-gin-norm": {
			QueriesFrom: "pg-gin",
			Params:      map[string]any{"rank_norm": "1"},
		},
	}}

	t.Run("an alias reads the source block but keeps its own params", func(t *testing.T) {
		b := s.QueryBinding("pg-gin-norm")
		assert.Equal(t, "pg-gin", b.QuerySource)
		assert.Equal(t, "1", b.Params["rank_norm"])
	})

	t.Run("a plain engine reads its own block", func(t *testing.T) {
		b := s.QueryBinding("pg-gin")
		assert.Equal(t, "pg-gin", b.QuerySource)
		assert.Equal(t, "0", b.Params["rank_norm"])
	})

	t.Run("an undeclared engine reads its own block", func(t *testing.T) {
		b := s.QueryBinding("elasticsearch")
		assert.Equal(t, "elasticsearch", b.QuerySource)
		assert.Empty(t, b.Params)
	})
}
