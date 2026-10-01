package suite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The model predicate and the query vector reach one template from different
// layers — the model from the engine's spec, the vector from the run — so a
// render that drops either would rank across two vector spaces.
func TestResolveEngineQuery_RendersModelAndVectorTogether(t *testing.T) {
	reg := NewTemplateRegistry()
	require.NoError(t, reg.Register(&QueryTemplate{
		ID: "cosine",
		Query: "SELECT article_id AS id FROM article_embeddings\n" +
			"WHERE model_name = '{{embedding_model}}'\n" +
			"ORDER BY embedding <=> '{{embedding}}'::vector\n" +
			"LIMIT {{limit}}",
	}))
	q := Query{
		ID: "sem-1",
		Engines: map[string]EngineQuery{
			"pgvector-cosine": {
				Template: "cosine",
				Params:   TemplateParams{"embedding": "{{" + ReservedQueryVectorParam + "}}", "limit": 50},
			},
		},
	}

	resolved, err := q.ResolveEngineQuery(ResolveOptions{
		Engine:   "pgvector-cosine",
		Registry: reg,
		Dialect:  DialectPostgres,
		Defaults: TemplateParams{EmbeddingModelParam: "qwen3-embedding:0.6b"},
		Extra:    TemplateParams{ReservedQueryVectorParam: "[0.1,0.2]"},
	})

	require.NoError(t, err)
	require.NotNil(t, resolved)
	assert.Contains(t, resolved.Query, "WHERE model_name = 'qwen3-embedding:0.6b'")
	assert.Contains(t, resolved.Query, "'[0.1,0.2]'::vector")
}

func TestResolveEngineQuery_EngineParamsReachInlineQuery(t *testing.T) {
	q := Query{
		ID: "sem-1",
		Engines: map[string]EngineQuery{
			"elasticsearch": {Query: `{"knn":{"field":"embedding_{{embedding_model}}"}}`},
		},
	}

	resolved, err := q.ResolveEngineQuery(ResolveOptions{
		Engine:   "elasticsearch",
		Defaults: TemplateParams{EmbeddingModelParam: "qwen3"},
		Dialect:  DialectJSON,
	})

	require.NoError(t, err)
	require.NotNil(t, resolved)
	assert.Equal(t, `{"knn":{"field":"embedding_qwen3"}}`, resolved.Query)
}
