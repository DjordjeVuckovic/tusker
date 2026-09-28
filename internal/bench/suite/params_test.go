package suite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rankTemplateRegistry(t *testing.T) *TemplateRegistry {
	t.Helper()
	reg := NewTemplateRegistry()
	require.NoError(t, reg.Register(&QueryTemplate{
		ID:    "pg_idx",
		Query: "SELECT id FROM articles WHERE search_vector @@ q('{{terms}}') ORDER BY ts_rank(search_vector, q('{{terms}}'), {{rank_norm}}) DESC LIMIT {{limit}}",
	}))
	return reg
}

func TestEngineQuery_Resolve_ParamPrecedence(t *testing.T) {
	reg := rankTemplateRegistry(t)

	tests := []struct {
		name     string
		defaults TemplateParams
		params   TemplateParams
		extra    TemplateParams
		want     string
		wantErr  string
	}{
		{
			name:     "engine defaults supply what the query omits",
			defaults: TemplateParams{"rank_norm": "1"},
			params:   TemplateParams{"terms": "climate", "limit": 10},
			want:     "ts_rank(search_vector, q('climate'), 1) DESC LIMIT 10",
		},
		{
			name:     "query params win over engine defaults",
			defaults: TemplateParams{"rank_norm": "1", "limit": 10},
			params:   TemplateParams{"terms": "climate", "rank_norm": "0", "limit": 50},
			want:     "ts_rank(search_vector, q('climate'), 0) DESC LIMIT 50",
		},
		{
			name:     "run-time extra wins over both",
			defaults: TemplateParams{"rank_norm": "1", "limit": 10},
			params:   TemplateParams{"terms": "climate", "rank_norm": "0", "limit": 50},
			extra:    TemplateParams{"limit": 100},
			want:     "ts_rank(search_vector, q('climate'), 0) DESC LIMIT 100",
		},
		{
			name:    "a param no layer supplies is named in the error",
			params:  TemplateParams{"terms": "climate", "limit": 10},
			wantErr: "rank_norm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eq := EngineQuery{Template: "pg_idx", Params: tt.params}
			resolved, err := eq.Resolve(ResolveOptions{
				Registry: reg,
				Defaults: tt.defaults,
				Extra:    tt.extra,
			})
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Contains(t, resolved.Query, tt.want)
		})
	}
}

// Two engines that differ only in a ranking argument share one query block, so
// the arms cannot drift apart in the suite.
func TestResolveEngineQuery_AliasedEnginesShareOneBlock(t *testing.T) {
	reg := rankTemplateRegistry(t)
	q := Query{
		ID: "qs-climate",
		Engines: map[string]EngineQuery{
			"pg-gin": {Template: "pg_idx", Params: TemplateParams{"terms": "climate change", "limit": 100}},
		},
	}

	unnormalised, err := q.ResolveEngineQuery(ResolveOptions{
		Engine:   "pg-gin",
		Registry: reg,
		Defaults: TemplateParams{"rank_norm": "0"},
	})
	require.NoError(t, err)

	// pg-gin-norm has no block of its own; queries_from sends it to pg-gin's.
	normalised, err := q.ResolveEngineQuery(ResolveOptions{
		Engine:   "pg-gin",
		Registry: reg,
		Defaults: TemplateParams{"rank_norm": "1"},
	})
	require.NoError(t, err)

	assert.Contains(t, unnormalised.Query, "q('climate change')")
	assert.Contains(t, normalised.Query, "q('climate change')")
	assert.Contains(t, unnormalised.Query, "q('climate change'), 0) DESC")
	assert.Contains(t, normalised.Query, "q('climate change'), 1) DESC")
}

func TestEngineQuery_Resolve_LeavesEngineDefaultsUnchanged(t *testing.T) {
	defaults := TemplateParams{"rank_norm": "0"}
	eq := EngineQuery{
		Query:  "SELECT {{rank_norm}}, {{limit}}",
		Params: TemplateParams{"rank_norm": "1", "limit": 5},
	}

	resolved, err := eq.Resolve(ResolveOptions{Defaults: defaults})
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1, 5", resolved.Query)
	assert.Equal(t, TemplateParams{"rank_norm": "0"}, defaults,
		"engine params are shared by every query the engine runs and must survive resolution")
}
