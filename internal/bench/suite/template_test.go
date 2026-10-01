package suite

import (
	"encoding/json"
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

	result, err := tmpl.Render(params, DialectPostgres)
	require.NoError(t, err)
	assert.Equal(t, "SELECT id FROM articles WHERE search_vector @@ plainto_tsquery('english', 'climate change') LIMIT 10", result.Query)
}

func TestQueryTemplate_Render_MissingParams(t *testing.T) {
	tmpl := &QueryTemplate{
		ID:    "fts_query",
		Query: "SELECT * WHERE lang = '{{lang}}' AND terms = '{{terms}}'",
	}

	_, err := tmpl.Render(TemplateParams{"lang": "english"}, DialectPostgres)
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

	result, err := reg.RenderQuery("fts", TemplateParams{"term": "hello"}, DialectPostgres)
	require.NoError(t, err)
	assert.Equal(t, "SELECT * WHERE term = 'hello'", result.Query)
}

func TestTemplateRegistry_RenderQuery_NotFound(t *testing.T) {
	reg := NewTemplateRegistry()

	_, err := reg.RenderQuery("unknown", nil, DialectPostgres)
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

func TestQueryTemplate_Render_BoundValues(t *testing.T) {
	awkward := `Ukraine's "trade war" don't`

	tests := []struct {
		name      string
		query     string
		params    TemplateParams
		dialect   Dialect
		wantQuery string
		wantArgs  []any
	}{
		{
			name:      "postgres binds the value as a positional argument",
			query:     "SELECT id FROM articles WHERE search_vector @@ plainto_tsquery('english', {{$terms}}) LIMIT {{limit}}",
			params:    TemplateParams{"terms": awkward, "limit": 10},
			dialect:   DialectPostgres,
			wantQuery: "SELECT id FROM articles WHERE search_vector @@ plainto_tsquery('english', $1) LIMIT 10",
			wantArgs:  []any{awkward},
		},
		{
			name:      "postgres reuses one argument for a repeated name",
			query:     "WHERE a @@ q({{$terms}}) OR b = lower({{$term}}) ORDER BY rank(q({{$terms}}))",
			params:    TemplateParams{"terms": "climate change", "term": "climat"},
			dialect:   DialectPostgres,
			wantQuery: "WHERE a @@ q($1) OR b = lower($2) ORDER BY rank(q($1))",
			wantArgs:  []any{"climate change", "climat"},
		},
		{
			name:      "structural params stay textual beside bound ones",
			query:     "WHERE to_tsvector('english', {{field}}) @@ {{tsquery_fn}}('english', {{$terms}})::tsquery",
			params:    TemplateParams{"field": "title || ' ' || coalesce(description,'')", "tsquery_fn": "plainto_tsquery", "terms": "don't"},
			dialect:   DialectPostgres,
			wantQuery: "WHERE to_tsvector('english', title || ' ' || coalesce(description,'')) @@ plainto_tsquery('english', $1)::tsquery",
			wantArgs:  []any{"don't"},
		},
		{
			name:      "a bound value that looks like a placeholder is never substituted",
			query:     "SELECT {{$terms}} LIMIT {{limit}}",
			params:    TemplateParams{"terms": "{{limit}}", "limit": 5},
			dialect:   DialectPostgres,
			wantQuery: "SELECT $1 LIMIT 5",
			wantArgs:  []any{"{{limit}}"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl := &QueryTemplate{ID: "t", Query: tt.query}

			result, err := tmpl.Render(tt.params, tt.dialect)

			require.NoError(t, err)
			assert.Equal(t, tt.wantQuery, result.Query)
			assert.Equal(t, tt.wantArgs, result.Args)
		})
	}
}

func TestQueryTemplate_Render_JSONInlinesBoundValueAsString(t *testing.T) {
	awkward := `Ukraine's "trade war" don't \ <b>`
	tmpl := &QueryTemplate{ID: "es", Query: `{"query": {"match": {"title": {{$terms}}}}, "size": {{limit}}}`}

	result, err := tmpl.Render(TemplateParams{"terms": awkward, "limit": 10}, DialectJSON)

	require.NoError(t, err)
	assert.Empty(t, result.Args)
	var body struct {
		Query struct {
			Match struct {
				Title string `json:"title"`
			} `json:"match"`
		} `json:"query"`
		Size int `json:"size"`
	}
	require.NoError(t, json.Unmarshal([]byte(result.Query), &body), "rendered body must be valid JSON: %s", result.Query)
	assert.Equal(t, awkward, body.Query.Match.Title)
	assert.Equal(t, 10, body.Size)
}

func TestQueryTemplate_Render_BoundValueErrors(t *testing.T) {
	tests := []struct {
		name    string
		params  TemplateParams
		dialect Dialect
		wantErr string
	}{
		{name: "missing bound value", params: TemplateParams{}, dialect: DialectPostgres, wantErr: "terms"},
		{name: "no dialect to bind with", params: TemplateParams{"terms": "climate"}, wantErr: "dialect"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl := &QueryTemplate{ID: "t", Query: "SELECT plainto_tsquery({{$terms}})"}

			_, err := tmpl.Render(tt.params, tt.dialect)

			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestQueryTemplate_RequiredParams_IncludesBoundNames(t *testing.T) {
	tmpl := &QueryTemplate{ID: "t", Query: "q({{$terms}}) LIMIT {{limit}}"}

	assert.ElementsMatch(t, []string{"terms", "limit"}, tmpl.RequiredParams())
}

func TestQuery_ResolveEngineQuery_InlineBindsValues(t *testing.T) {
	q := Query{
		ID: "q1",
		Engines: map[string]EngineQuery{
			"pg": {Query: "SELECT id FROM articles WHERE title = {{$terms}}", Params: TemplateParams{"terms": "don't"}},
			"es": {Query: `{"query": {"match": {"title": {{$terms}}}}}`, Params: TemplateParams{"terms": "don't"}},
		},
	}

	pg, err := q.ResolveEngineQuery(ResolveOptions{Engine: "pg", Dialect: DialectPostgres})
	require.NoError(t, err)
	assert.Equal(t, "SELECT id FROM articles WHERE title = $1", pg.Query)
	assert.Equal(t, []any{"don't"}, pg.Args)

	es, err := q.ResolveEngineQuery(ResolveOptions{Engine: "es", Dialect: DialectJSON})
	require.NoError(t, err)
	assert.JSONEq(t, `{"query": {"match": {"title": "don't"}}}`, es.Query)
}

func TestQuery_ResolveEngineQuery_BindsNonStringScalarsAsText(t *testing.T) {
	tests := []struct {
		yamlValue string
		wantText  string
	}{
		{yamlValue: "2024", wantText: "2024"},
		{yamlValue: "true", wantText: "true"},
		{yamlValue: "1.5", wantText: "1.5"},
		{yamlValue: "[climate, change]", wantText: "climate, change"},
	}
	for _, tt := range tests {
		t.Run(tt.yamlValue, func(t *testing.T) {
			loaded, err := Parse([]byte(`schema_version: 1
id: scalars
templates:
  - id: pg_fts
    query: "SELECT id FROM articles WHERE search_vector @@ plainto_tsquery('english', {{$terms}})"
  - id: es_match
    query: '{"query": {"match": {"title": {{$terms}}}}}'
queries:
  - id: q1
    engines:
      pg: { template: pg_fts, params: { terms: ` + tt.yamlValue + ` } }
      es: { template: es_match, params: { terms: ` + tt.yamlValue + ` } }
`))
			require.NoError(t, err)
			q := loaded.Suite.Queries[0]

			pg, err := q.ResolveEngineQuery(ResolveOptions{Engine: "pg", Registry: loaded.Registry, Dialect: DialectPostgres})
			require.NoError(t, err)
			assert.Equal(t, []any{tt.wantText}, pg.Args)

			es, err := q.ResolveEngineQuery(ResolveOptions{Engine: "es", Registry: loaded.Registry, Dialect: DialectJSON})
			require.NoError(t, err)
			var body struct {
				Query struct {
					Match struct {
						Title string `json:"title"`
					} `json:"match"`
				} `json:"query"`
			}
			require.NoError(t, json.Unmarshal([]byte(es.Query), &body), "body must carry the value as a JSON string: %s", es.Query)
			assert.Equal(t, tt.wantText, body.Query.Match.Title)
		})
	}
}

func TestQueryTemplate_Render_RejectsParamBothBoundAndPasted(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		params TemplateParams
	}{
		{
			name:   "pasted directly",
			query:  "SELECT id FROM articles WHERE search_vector @@ plainto_tsquery('english', {{$terms}}) OR title = '{{terms}}'",
			params: TemplateParams{"terms": "don't"},
		},
		{
			name:   "pasted through another param's value",
			query:  "SELECT id FROM articles WHERE {{filter}} AND search_vector @@ plainto_tsquery('english', {{$terms}})",
			params: TemplateParams{"terms": "don't", "filter": "title = '{{terms}}'"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl := &QueryTemplate{ID: "pg_mixed", Query: tt.query}

			_, err := tmpl.Render(tt.params, DialectPostgres)

			require.Error(t, err)
			assert.ErrorContains(t, err, "pg_mixed")
			assert.ErrorContains(t, err, "terms")
		})
	}
}
