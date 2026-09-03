package suite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryTemplate_Render(t *testing.T) {
	tmpl := &QueryTemplate{
		ID:    "fts_query",
		Query: "SELECT id FROM articles WHERE search_vector @@ plainto_tsquery('{{lang}}', '{{terms}}') LIMIT {{limit}}",
	}

	params := TemplateParams{
		"lang":  "english",
		"terms": "climate change",
		"limit": 10,
	}

	result, err := tmpl.Render(params, "")
	require.NoError(t, err)
	assert.Equal(t, "SELECT id FROM articles WHERE search_vector @@ plainto_tsquery('english', 'climate change') LIMIT 10", result.Query)
}

func TestQueryTemplate_Render_MissingParams(t *testing.T) {
	tmpl := &QueryTemplate{
		ID:    "fts_query",
		Query: "SELECT * WHERE lang = '{{lang}}' AND terms = '{{terms}}'",
	}

	_, err := tmpl.Render(TemplateParams{"lang": "english"}, "")
	assert.ErrorContains(t, err, "missing params")
	assert.ErrorContains(t, err, "terms")
}

func TestQueryTemplate_RequiredParams(t *testing.T) {
	tmpl := &QueryTemplate{
		ID:    "fts_query",
		Query: "{{lang}} {{terms}} {{limit}}",
	}

	params := tmpl.RequiredParams()
	assert.Len(t, params, 3)
	assert.Contains(t, params, "lang")
	assert.Contains(t, params, "terms")
	assert.Contains(t, params, "limit")
}

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

func TestTemplateRegistry_RenderQuery(t *testing.T) {
	reg := NewTemplateRegistry()

	tmpl := &QueryTemplate{ID: "fts", Query: "SELECT * WHERE term = '{{term}}'"}
	require.NoError(t, reg.Register(tmpl))

	result, err := reg.RenderQuery("fts", TemplateParams{"term": "hello"}, "")
	require.NoError(t, err)
	assert.Equal(t, "SELECT * WHERE term = 'hello'", result.Query)
}

func TestTemplateRegistry_RenderQuery_NotFound(t *testing.T) {
	reg := NewTemplateRegistry()

	_, err := reg.RenderQuery("unknown", nil, "")
	assert.ErrorContains(t, err, "not found")
}

func TestFormatValue(t *testing.T) {
	tests := []struct {
		input    any
		expected string
	}{
		{"hello", "hello"},
		{42, "42"},
		{int64(100), "100"},
		{3.14, "3.14"},
		{true, "true"},
		{false, "false"},
		{[]string{"a", "b", "c"}, "a, b, c"},
		{[]any{"x", 1, true}, "x, 1, true"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, formatValue(tt.input))
		})
	}
}

func TestQuery_ResolveEngineQuery_Template(t *testing.T) {
	reg := NewTemplateRegistry()
	tmpl := &QueryTemplate{ID: "fts_basic", Query: "SELECT * FROM articles WHERE term = '{{term}}'"}
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
	assert.Equal(t, "SELECT * FROM articles WHERE term = 'climate'", result.Query)
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
	tmpl := &QueryTemplate{ID: "fts", Query: "SELECT * WHERE term = '{{term}}'"}
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
	assert.Equal(t, "SELECT * WHERE term = 'climate'", pgResult.Query)

	esResult, err := q.ResolveEngineQuery(ResolveOptions{Engine: "es", Registry: reg})
	require.NoError(t, err)
	require.NotNil(t, esResult)
	assert.Equal(t, `{"query":"climate"}`, esResult.Query)
}
