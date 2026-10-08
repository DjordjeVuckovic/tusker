package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeValidateAPI answers <index>/_validate/query the way Elasticsearch does:
// it rejects knn and any top-level key besides query, and explains an unknown
// query type.
func fakeValidateAPI(t *testing.T) *httptest.Server {
	t.Helper()
	es := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.True(t, strings.HasSuffix(r.URL.Path, "/_validate/query"), r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		var body map[string]json.RawMessage
		assert.NoError(t, json.Unmarshal(raw, &body))
		switch {
		case body["knn"] != nil:
			_, _ = w.Write([]byte(`{"valid": false, "explanations": [{"error": "request does not support [knn]"}]}`))
		case len(body) > 1:
			_, _ = w.Write([]byte(`{"valid": false}`))
		case strings.Contains(string(body["query"]), "bogus"):
			_, _ = w.Write([]byte(`{"valid": false, "explanations": [{"error": "unknown query [bogus]"}]}`))
		default:
			_, _ = w.Write([]byte(`{"valid": true}`))
		}
	}))
	t.Cleanup(es.Close)
	return es
}

func TestEsExecutor_Validate(t *testing.T) {
	const vector = `"field": "embedding", "query_vector": [0.1, 0.2], "k": 10`
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "query DSL", body: `{"query": {"match_all": {}}}`},
		{name: "query with search options", body: `{"query": {"match_all": {}}, "size": 10}`},
		{name: "unknown query type", body: `{"query": {"bogus": {}}}`, wantErr: "bogus"},
		{name: "knn only", body: `{"knn": {` + vector + `}}`},
		{name: "query and knn", body: `{"query": {"match_all": {}}, "knn": {` + vector + `}}`},
		{name: "knn array", body: `{"knn": [{` + vector + `}, {` + vector + `}]}`},
		{name: "knn embeds the query itself", body: `{"knn": {"field": "embedding", "query_vector_builder": {"text_embedding": {}}}}`},
		{name: "knn without a field", body: `{"knn": {"query_vector": [0.1]}}`, wantErr: `"field"`},
		{name: "knn with an empty field", body: `{"knn": {"field": "", "query_vector": [0.1]}}`, wantErr: "non-empty"},
		{name: "knn without a vector", body: `{"knn": {"field": "embedding", "k": 10}}`, wantErr: "query_vector"},
		{name: "empty knn array", body: `{"knn": []}`, wantErr: "empty"},
		{name: "bad entry in a knn array", body: `{"knn": [{` + vector + `}, {"field": "e"}]}`, wantErr: "knn[1]"},
		{name: "knn beside a broken query", body: `{"query": {"bogus": {}}, "knn": {` + vector + `}}`, wantErr: "bogus"},
	}
	es := fakeValidateAPI(t)
	exec := NewEsExecutor("es", es.URL, "articles")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := exec.Validate(context.Background(), Request{Query: tt.body})

			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestEsExecutor_Execute_ReportsCorpusMatchesOnlyWhenCounted(t *testing.T) {
	docID := uuid.New()
	tests := []struct {
		name  string
		total string
		want  *int64
	}{
		{name: "counted every match", total: `{"value": 2466, "relation": "eq"}`, want: ptr(int64(2466))},
		{name: "stopped counting at track_total_hits", total: `{"value": 10000, "relation": "gte"}`},
		{name: "relation absent", total: `{"value": 42}`, want: ptr(int64(42))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			es := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"hits": {"total": ` + tt.total + `, "hits": [{"_source": {"id": "` + docID.String() + `"}}]}}`))
			}))
			defer es.Close()

			got, err := NewEsExecutor("es", es.URL, "articles").Execute(context.Background(), Request{Query: `{"query": {"match_all": {}}}`})

			require.NoError(t, err)
			assert.Equal(t, []uuid.UUID{docID}, got.RankedDocIDs)
			assert.Equal(t, tt.want, got.CorpusMatches)
		})
	}
}

func ptr[T any](v T) *T { return &v }
