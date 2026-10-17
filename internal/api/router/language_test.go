package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	apiserver "github.com/DjordjeVuckovic/tusker/internal/api/server"
	"github.com/DjordjeVuckovic/tusker/internal/apperr"
	"github.com/labstack/echo/v4"
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
		requests := map[string]*http.Request{
			"query string": httptest.NewRequest(http.MethodGet, "/v1/articles/search?q=climate&lang="+lang.value, nil),
		}
		for queryType, body := range structured {
			requests[queryType] = jsonRequest(http.MethodPost, "/v1/articles/_search", fmt.Sprintf(body, lang.value))
		}
		for name, req := range requests {
			t.Run(name+" "+lang.value, func(t *testing.T) {
				e := echo.New()
				(&apiserver.Server{Echo: e}).SetupValidator()
				e.HTTPErrorHandler = apperr.GlobalErrorHandler()
				NewSearchRouter(e, stubFtsSearcher{caps: elasticsearchCaps}, WithHybridSearcher(stubHybridSearcher{})).Bind()

				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, req)

				if rec.Code != lang.wantCode {
					t.Errorf("status = %d, want %d (body: %s)", rec.Code, lang.wantCode, rec.Body.String())
				}
			})
		}
	}
}
