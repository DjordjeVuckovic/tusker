package router

import (
	"fmt"
	"net/http"
	"testing"
)

func TestSearchHandlers_TakeTheLanguageAsAnISO6393Code(t *testing.T) {
	structured := map[string]string{
		"match":       `{"query":{"match":{"field":"title","query":"climate","language":%q}}}`,
		"multi_match": `{"query":{"multi_match":{"fields":["title"],"query":"climate","language":%q}}}`,
		"phrase":      `{"query":{"phrase":{"fields":["title"],"query":"climate change","language":%q}}}`,
		"boolean":     `{"query":{"boolean":{"expression":"climate AND change","language":%q}}}`,
		"hybrid":      `{"query":{"hybrid":{"query":"climate","language":%q}}}`,
	}
	languages := []struct {
		value    string
		wantCode int
	}{
		{value: "eng", wantCode: http.StatusOK},
		{value: "english", wantCode: http.StatusBadRequest},
		{value: "srp", wantCode: http.StatusBadRequest},
		{value: "en", wantCode: http.StatusBadRequest},
	}

	for _, lang := range languages {
		calls := map[string]searchCall{
			"query string": {method: http.MethodGet, target: "/v1/articles/search?q=climate&lang=" + lang.value},
		}
		for queryType, body := range structured {
			calls[queryType] = searchCall{body: fmt.Sprintf(body, lang.value)}
		}
		for name, call := range calls {
			t.Run(name+" "+lang.value, func(t *testing.T) {
				call.caps = elasticsearchCaps
				rec := serveSearch(t, call)
				if rec.Code != lang.wantCode {
					t.Errorf("status = %d, want %d (body: %s)", rec.Code, lang.wantCode, rec.Body.String())
				}
			})
		}
	}
}
