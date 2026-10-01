package suite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryTemplate_Validate(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    *QueryTemplate
		wantErr bool
	}{
		{
			name:    "valid template",
			tmpl:    &QueryTemplate{ID: "test", Query: "SELECT 1"},
			wantErr: false,
		},
		{
			name:    "missing id",
			tmpl:    &QueryTemplate{Query: "SELECT 1"},
			wantErr: true,
		},
		{
			name:    "no query",
			tmpl:    &QueryTemplate{ID: "test"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.tmpl.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTemplateRegistry_Register(t *testing.T) {
	reg := NewTemplateRegistry()

	tmpl := &QueryTemplate{ID: "test", Query: "SELECT 1"}
	err := reg.Register(tmpl)
	require.NoError(t, err)

	got, ok := reg.Get("test")
	assert.True(t, ok)
	assert.Equal(t, tmpl, got)
}

func TestTemplateRegistry_RegisterDuplicate(t *testing.T) {
	reg := NewTemplateRegistry()

	tmpl := &QueryTemplate{ID: "test", Query: "SELECT 1"}
	err := reg.Register(tmpl)
	require.NoError(t, err)

	err = reg.Register(tmpl)
	assert.ErrorContains(t, err, "already registered")
}

func TestQuery_ResolveEngineQuery_Template(t *testing.T) {
	reg := NewTemplateRegistry()
	tmpl := &QueryTemplate{ID: "fts_basic", Args: []string{"term"}, Query: "SELECT * FROM articles WHERE term = $1"}
	require.NoError(t, reg.Register(tmpl))

	q := Query{
		ID: "q1",
		Engines: map[string]EngineQuery{
			"pg": {Template: "fts_basic", Params: TemplateParams{"term": "climate"}},
		},
	}

	result, err := q.ResolveEngineQuery(ResolveOptions{Engine: "pg", Registry: reg})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "SELECT * FROM articles WHERE term = $1", result.Query)
	assert.Equal(t, []any{"climate"}, result.Args)
}

func TestQuery_ResolveEngineQuery_Inline(t *testing.T) {
	q := Query{
		ID: "q1",
		Engines: map[string]EngineQuery{
			"pg": {Query: "SELECT 1"},
			"es": {Query: `{"query": "test"}`},
		},
	}

	pgResult, err := q.ResolveEngineQuery(ResolveOptions{Engine: "pg"})
	require.NoError(t, err)
	require.NotNil(t, pgResult)
	assert.Equal(t, "SELECT 1", pgResult.Query)

	esResult, err := q.ResolveEngineQuery(ResolveOptions{Engine: "es"})
	require.NoError(t, err)
	require.NotNil(t, esResult)
	assert.Equal(t, `{"query": "test"}`, esResult.Query)
}

func TestQuery_ResolveEngineQuery_NotFound(t *testing.T) {
	q := Query{
		ID: "q1",
		Engines: map[string]EngineQuery{
			"pg": {Query: "SELECT 1"},
		},
	}

	result, err := q.ResolveEngineQuery(ResolveOptions{Engine: "unknown"})
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestQuery_ResolveEngineQuery_MixedEngines(t *testing.T) {
	reg := NewTemplateRegistry()
	tmpl := &QueryTemplate{ID: "fts", Args: []string{"term"}, Query: "SELECT * WHERE term = $1"}
	require.NoError(t, reg.Register(tmpl))

	q := Query{
		ID: "q1",
		Engines: map[string]EngineQuery{
			"pg": {Template: "fts", Params: TemplateParams{"term": "climate"}},
			"es": {Query: `{"query":"climate"}`},
		},
	}

	pgResult, err := q.ResolveEngineQuery(ResolveOptions{Engine: "pg", Registry: reg})
	require.NoError(t, err)
	require.NotNil(t, pgResult)
	assert.Equal(t, "SELECT * WHERE term = $1", pgResult.Query)
	assert.Equal(t, []any{"climate"}, pgResult.Args)

	esResult, err := q.ResolveEngineQuery(ResolveOptions{Engine: "es", Registry: reg})
	require.NoError(t, err)
	require.NotNil(t, esResult)
	assert.Equal(t, `{"query":"climate"}`, esResult.Query)
}
