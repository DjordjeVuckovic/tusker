package suite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngineQuery_Resolve_ParamPrecedence(t *testing.T) {
	block := EngineQuery{
		Query: "SELECT id FROM articles ORDER BY ts_rank(search_vector, plainto_tsquery($1), $2::int) DESC LIMIT $3::int",
		Args:  []string{"terms", "rank_norm", "limit"},
	}

	tests := []struct {
		name     string
		defaults TemplateParams
		params   TemplateParams
		want     []any
		wantErr  string
	}{
		{
			name:     "engine defaults supply what the query omits",
			defaults: TemplateParams{"rank_norm": "1"},
			params:   TemplateParams{"terms": "climate", "limit": 10},
			want:     []any{"climate", "1", 10},
		},
		{
			name:     "query params win over engine defaults",
			defaults: TemplateParams{"rank_norm": "1", "limit": 10},
			params:   TemplateParams{"terms": "climate", "rank_norm": "0", "limit": 50},
			want:     []any{"climate", "0", 50},
		},
		{
			name:    "an arg no layer supplies is named in the error",
			params:  TemplateParams{"terms": "climate", "limit": 10},
			wantErr: "rank_norm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eq := block
			eq.Params = tt.params

			resolved, err := eq.Resolve(ResolveOptions{Defaults: tt.defaults})

			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, resolved.Args)
		})
	}
}

// A value is handed to the engine as it is; nothing in it is read as syntax, so
// one param's value can never fill in another's.
func TestEngineQuery_Resolve_PassesValuesThroughVerbatim(t *testing.T) {
	eq := EngineQuery{
		Query:  "SELECT id FROM articles WHERE title = $1 LIMIT $2::int",
		Args:   []string{"terms", "limit"},
		Params: TemplateParams{"terms": "{{limit}} $2", "limit": 5},
	}

	resolved, err := eq.Resolve(ResolveOptions{})

	require.NoError(t, err)
	assert.Equal(t, []any{"{{limit}} $2", 5}, resolved.Args)
	assert.Equal(t, "SELECT id FROM articles WHERE title = $1 LIMIT $2::int", resolved.Query)
}

func TestEngineQuery_Resolve_LeavesEngineDefaultsUnchanged(t *testing.T) {
	defaults := TemplateParams{"rank_norm": "0"}
	eq := EngineQuery{
		Query:  "SELECT $1::int, $2::int",
		Args:   []string{"rank_norm", "limit"},
		Params: TemplateParams{"rank_norm": "1", "limit": 5},
	}

	resolved, err := eq.Resolve(ResolveOptions{Defaults: defaults, QueryVector: []float32{0.5}})

	require.NoError(t, err)
	assert.Equal(t, []any{"1", 5}, resolved.Args)
	assert.Equal(t, TemplateParams{"rank_norm": "0"}, defaults,
		"engine params are shared by every query the engine runs and must survive resolution")
}

func TestParse_RejectsArgsThatDoNotFitTheStatement(t *testing.T) {
	tests := []struct {
		name    string
		suite   string
		wantErr []string
	}{
		{
			name: "query gives a param its template takes no arg for",
			suite: `templates:
  - id: pg_idx
    args: [terms]
    query: "SELECT id FROM articles WHERE search_vector @@ plainto_tsquery($1)"
queries:
  - id: q1
    engines:
      pg: { template: pg_idx, params: { terms: climate, field: title } }
`,
			wantErr: []string{"q1", "pg", "field"},
		},
		{
			name: "inline query gives params but declares no args",
			suite: `queries:
  - id: q1
    engines:
      pg:
        query: "SELECT id FROM articles LIMIT 10"
        params: { limit: 10 }
`,
			wantErr: []string{"q1", "pg", "limit"},
		},
		{
			name: "query sets the reserved query vector itself",
			suite: `templates:
  - id: pgvec
    args: [query_vector]
    query: "SELECT article_id AS id FROM article_embeddings ORDER BY embedding <=> $1::vector"
queries:
  - id: q1
    engines:
      pg: { template: pgvec, params: { query_vector: "[0.1]" } }
`,
			wantErr: []string{"q1", "pg", QueryVectorArg},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(validHeader + tt.suite))

			require.Error(t, err)
			for _, want := range tt.wantErr {
				assert.ErrorContains(t, err, want)
			}
		})
	}
}

func TestLoadedSuite_CheckArgsSupplied(t *testing.T) {
	loaded, err := Parse([]byte(validHeader + `templates:
  - id: pg_idx
    args: [terms, rank_norm, limit]
    query: "SELECT id FROM articles ORDER BY ts_rank(search_vector, plainto_tsquery($1), $2::int) DESC LIMIT $3::int"
  - id: pgvec
    args: [embedding_model, query_vector, limit]
    query: "SELECT article_id AS id FROM article_embeddings WHERE model_name = $1 ORDER BY embedding <=> $2::vector LIMIT $3::int"
queries:
  - id: qs-climate
    engines:
      pg-gin: { template: pg_idx, params: { terms: climate, limit: 100 } }
      pgvector: { template: pgvec, params: { limit: 50 } }
`))
	require.NoError(t, err)

	tests := []struct {
		name     string
		engine   string
		defaults TemplateParams
		wantErr  []string
	}{
		{name: "engine params supply the rest", engine: "pg-gin", defaults: TemplateParams{"rank_norm": "0"}},
		{name: "no layer supplies an arg", engine: "pg-gin", wantErr: []string{"qs-climate", "pg-gin", "rank_norm"}},
		{name: "the run supplies the query vector", engine: "pgvector", defaults: TemplateParams{EmbeddingModelParam: "qwen3"}},
		{name: "an engine the suite has no block for", engine: "elasticsearch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := loaded.CheckArgsSupplied(tt.engine, tt.defaults)

			if len(tt.wantErr) == 0 {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			for _, want := range tt.wantErr {
				assert.ErrorContains(t, err, want)
			}
		})
	}
}

func TestResolveEngineQuery_FillsQueryVectorArg(t *testing.T) {
	loaded, err := Parse([]byte(validHeader + `templates:
  - id: pgvec
    args: [embedding_model, query_vector, limit]
    query: "SELECT article_id AS id FROM article_embeddings WHERE model_name = $1 ORDER BY embedding <=> $2::vector LIMIT $3::int"
queries:
  - id: sem-1
    engines:
      pgvector-cosine: { template: pgvec, params: { limit: 50 } }
`))
	require.NoError(t, err)

	resolved, err := loaded.Suite.Queries[0].ResolveEngineQuery(ResolveOptions{
		Engine:      "pgvector-cosine",
		Registry:    loaded.Registry,
		Defaults:    TemplateParams{EmbeddingModelParam: "qwen3-embedding:0.6b"},
		QueryVector: []float32{0.1, -0.25},
	})

	require.NoError(t, err)
	assert.Equal(t, []any{"qwen3-embedding:0.6b", []float32{0.1, -0.25}, 50}, resolved.Args)
}

func TestResolveEngineQuery_QueryVectorArgWithoutVectorErrors(t *testing.T) {
	eq := EngineQuery{
		Query: "SELECT article_id AS id FROM article_embeddings ORDER BY embedding <=> $1::vector",
		Args:  []string{QueryVectorArg},
	}

	_, err := eq.Resolve(ResolveOptions{})

	assert.ErrorContains(t, err, QueryVectorArg)
}

func TestLoadedSuite_NeedsQueryVector(t *testing.T) {
	loaded, err := Parse([]byte(validHeader + `templates:
  - id: pgvec
    args: [query_vector]
    query: "SELECT article_id AS id FROM article_embeddings ORDER BY embedding <=> $1::vector"
queries:
  - id: through-template
    engines:
      pg: { template: pgvec }
  - id: through-inline-args
    engines:
      pg:
        query: "SELECT article_id AS id FROM article_embeddings ORDER BY embedding <=> $1::vector"
        args: [query_vector]
  - id: lexical
    engines:
      pg: "SELECT id FROM articles LIMIT 10"
`))
	require.NoError(t, err)

	want := map[string]bool{"through-template": true, "through-inline-args": true, "lexical": false}
	for i := range loaded.Suite.Queries {
		q := &loaded.Suite.Queries[i]
		assert.Equal(t, want[q.ID], loaded.NeedsQueryVector(q), q.ID)
	}
}

// A search template renders from named params, so a template query hands them
// over as values with their own types, the query vector as numbers.
func TestResolveEngineQuery_HandsTemplateParamsToASearchTemplate(t *testing.T) {
	loaded, err := Parse([]byte(validHeader + `templates:
  - id: es_knn
    args: [terms, query_vector, size]
    query: '{"query": {"match": {"title": "{{terms}}"}}, "knn": {"field": "embedding", "query_vector": {{#toJson}}query_vector{{/toJson}}}, "size": {{size}}}'
queries:
  - id: hybrid-1
    engines:
      elasticsearch: { template: es_knn, params: { terms: "don't", size: 50 } }
`))
	require.NoError(t, err)

	resolved, err := loaded.Suite.Queries[0].ResolveEngineQuery(ResolveOptions{
		Engine:      "elasticsearch",
		Registry:    loaded.Registry,
		Defaults:    TemplateParams{EmbeddingModelParam: "qwen3"},
		QueryVector: []float32{0.5, -1},
	})

	require.NoError(t, err)
	assert.Equal(t, "es_knn", resolved.Template)
	assert.Equal(t, TemplateParams{
		"terms":             "don't",
		"size":              50,
		QueryVectorArg:      []float32{0.5, -1},
		EmbeddingModelParam: "qwen3",
	}, resolved.Params)
}

func TestResolveEngineQuery_InlineQueryNamesNoTemplate(t *testing.T) {
	q := Query{ID: "q1", Engines: map[string]EngineQuery{"es": {Query: `{"query": {"match_all": {}}}`}}}

	resolved, err := q.ResolveEngineQuery(ResolveOptions{Engine: "es"})

	require.NoError(t, err)
	assert.Empty(t, resolved.Template)
}
