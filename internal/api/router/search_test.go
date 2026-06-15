package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apiserver "github.com/DjordjeVuckovic/tusker/internal/api/server"
	"github.com/DjordjeVuckovic/tusker/internal/apperr"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	dquery "github.com/DjordjeVuckovic/tusker/internal/types/query"
	"github.com/labstack/echo/v4"
)

type stubSemanticSearcher struct{}

func (stubSemanticSearcher) SearchSemantic(context.Context, *dquery.Semantic, *dquery.BaseOptions) (*storage.VectorSearchResult, error) {
	return &storage.VectorSearchResult{}, nil
}

type stubFtsSearcher struct{}

func (stubFtsSearcher) SearchStringQuery(context.Context, *dquery.String, *dquery.BaseOptions) (*storage.SearchResult, error) {
	return &storage.SearchResult{}, nil
}
func (stubFtsSearcher) SearchField(context.Context, *dquery.Match, *dquery.BaseOptions) (*storage.SearchResult, error) {
	return &storage.SearchResult{}, nil
}
func (stubFtsSearcher) SearchFields(context.Context, *dquery.MultiMatch, *dquery.BaseOptions) (*storage.SearchResult, error) {
	return &storage.SearchResult{}, nil
}
func (stubFtsSearcher) SearchPhrase(context.Context, *dquery.Phrase, *dquery.BaseOptions) (*storage.SearchResult, error) {
	return &storage.SearchResult{}, nil
}
func (stubFtsSearcher) SearchBoolean(context.Context, *dquery.Boolean, *dquery.BaseOptions) (*storage.SearchResult, error) {
	return &storage.SearchResult{}, nil
}

func TestStructuredSearchHandlerValidation(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{
			name:     "valid match request",
			body:     `{"size":10,"query":{"match":{"field":"title","query":"climate change","operator":"and"}}}`,
			wantCode: http.StatusOK,
		},
		{
			name:     "invalid match request missing query",
			body:     `{"query":{"match":{"field":"title","query":""}}}`,
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			(&apiserver.Server{Echo: e}).SetupValidator()
			e.HTTPErrorHandler = apperr.GlobalErrorHandler()

			r := &SearchRouter{e: e, searcher: stubFtsSearcher{}}
			r.Bind()

			req := httptest.NewRequest(http.MethodPost, "/v1/articles/_search", strings.NewReader(tt.body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantCode, rec.Body.String())
			}
		})
	}
}

func TestCapabilitiesHandler(t *testing.T) {
	tests := []struct {
		name         string
		withSemantic bool
		wantSemantic bool
	}{
		{name: "without semantic searcher", withSemantic: false, wantSemantic: false},
		{name: "with semantic searcher", withSemantic: true, wantSemantic: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &SearchRouter{e: echo.New()}
			if tt.withSemantic {
				r.semanticSearcher = stubSemanticSearcher{}
			}

			req := httptest.NewRequest(http.MethodGet, "/v1/capabilities", nil)
			rec := httptest.NewRecorder()
			c := r.e.NewContext(req, rec)

			if err := r.capabilitiesHandler(c); err != nil {
				t.Fatalf("capabilitiesHandler returned error: %v", err)
			}

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}

			var got map[string]bool
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("failed to decode body: %v", err)
			}

			for _, key := range []string{"string_query", "match", "multi_match", "phrase", "boolean"} {
				if !got[key] {
					t.Errorf("%q = false, want true", key)
				}
			}
			if got["semantic"] != tt.wantSemantic {
				t.Errorf("semantic = %v, want %v", got["semantic"], tt.wantSemantic)
			}
		})
	}
}
