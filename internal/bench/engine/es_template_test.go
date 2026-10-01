package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchTemplateID(t *testing.T) {
	assert.Equal(t, "news_hybrid-es_hybrid", SearchTemplateID("news_hybrid", "es_hybrid"))
	assert.NotEqual(t, SearchTemplateID("fts_quality", "es_match"), SearchTemplateID("news_fuzzy", "es_match"),
		"two tracks sharing a template id must not overwrite each other's stored template")
}

func TestEsExecutor_RegisterSearchTemplate_StoresMustacheSourceUnderItsID(t *testing.T) {
	const source = `{"query": {"match": {"title": "{{terms}}"}}, "size": {{size}}}`
	var gotMethod, gotPath string
	var gotBody struct {
		Script struct {
			Lang   string `json:"lang"`
			Source string `json:"source"`
		} `json:"script"`
	}
	es := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		_, _ = w.Write([]byte(`{"acknowledged": true}`))
	}))
	defer es.Close()

	exec := NewEsExecutor("es", es.URL, "articles")
	err := exec.RegisterSearchTemplate(context.Background(), SearchTemplate{
		ID:     SearchTemplateID("news/fts", "es_match"),
		Source: source,
	})

	require.NoError(t, err)
	assert.Equal(t, http.MethodPut, gotMethod)
	assert.Equal(t, "/_scripts/news%2Ffts-es_match", gotPath)
	assert.Equal(t, "mustache", gotBody.Script.Lang)
	assert.Equal(t, source, gotBody.Script.Source)
}

func TestEsExecutor_RegisterSearchTemplate_ReportsRejection(t *testing.T) {
	es := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "compile error"}`))
	}))
	defer es.Close()

	err := NewEsExecutor("es", es.URL, "articles").RegisterSearchTemplate(context.Background(),
		SearchTemplate{ID: "t-broken", Source: `{{#unclosed}}`})

	assert.ErrorContains(t, err, "t-broken")
	assert.ErrorContains(t, err, "compile error")
}
