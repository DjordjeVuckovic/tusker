package router

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/DjordjeVuckovic/tusker/internal/api/dto"
	"github.com/DjordjeVuckovic/tusker/internal/apperr"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	dquery "github.com/DjordjeVuckovic/tusker/internal/types/query"
	"github.com/DjordjeVuckovic/tusker/pkg/pagination"
	"github.com/labstack/echo/v4"
)

type SearchRouter struct {
	e                *echo.Echo
	searcher         storage.FtsSearcher
	semanticSearcher storage.SemanticSearcher
	hybridSearcher   storage.HybridSearcher
}

type SearchRouterOption func(*SearchRouter)

func NewSearchRouter(e *echo.Echo, searcher storage.FtsSearcher, opts ...SearchRouterOption) *SearchRouter {
	router := &SearchRouter{
		e:        e,
		searcher: searcher,
	}

	for _, opt := range opts {
		opt(router)
	}

	return router
}

func WithSemanticSearcher(searcher storage.SemanticSearcher) SearchRouterOption {
	return func(r *SearchRouter) {
		r.semanticSearcher = searcher
	}
}

func WithHybridSearcher(searcher storage.HybridSearcher) SearchRouterOption {
	return func(r *SearchRouter) {
		r.hybridSearcher = searcher
	}
}

func (r *SearchRouter) Bind() {
	// Simple query_string API (application-determined fields/weights)
	r.e.GET("/v1/articles/search", r.searchHandler)

	// Unified structured search API (match/multi_match with query wrapper)
	r.e.POST("/v1/articles/_search", r.structuredSearchHandler)

	// Semantic search endpoint (only if provided via options)
	if r.semanticSearcher != nil {
		r.e.GET("/v1/articles/semantic_search", r.handleSematicQuery)
	}

	// Capabilities discovery endpoint
	r.e.GET("/v1/capabilities", r.capabilitiesHandler)
}

// capabilitiesHandler reports which search paradigms the running backend supports (GET)
//
// The full-text searcher is always wired, so string_query, match, multi_match,
// phrase and boolean are always available. Semantic search is reported as
// available only when a semantic searcher has been wired into the router.
//
// @Summary Report supported search paradigms
// @Description Returns the search paradigms exposed by the running backend. Useful for clients to discover available capabilities (e.g. whether semantic search is enabled).
// @Tags capabilities
// @Produce json
// @Success 200 {object} dquery.Capabilities "Supported search paradigms"
// @Router /v1/capabilities [get]
func (r *SearchRouter) capabilitiesHandler(c echo.Context) error {
	caps := dquery.Capabilities{
		StringQuery: true,
		Match:       true,
		MultiMatch:  true,
		Phrase:      true,
		Boolean:     true,
		Semantic:    r.semanticSearcher != nil,
	}

	return c.JSON(http.StatusOK, caps)
}

// searchHandler handles simple query string search (GET)
//
// This endpoint provides a simple, Google-like search experience.
// The application automatically determines optimal fields and weights.
// Results are cacheable and URLs are bookmarkable.
//
// @Summary Simple query string search
// @Description Simple text search with automatic field selection and weighting. Cacheable and bookmarkable. Application determines optimal search strategy based on index configuration.
// @Tags search
// @Accept json
// @Produce json
// @Param q query string true "SearchStringQuery query text" example("climate change")
// @Param size query int false "Results per page (default: 100, max: 10000)" example(10)
// @Param cursor query string false "Pagination cursor (base64-encoded from previous response)"
// @Param lang query string false "SearchStringQuery language: english, serbian (default: english)" example("english")
// @Success 200 {object} dto.SearchResponse "SearchStringQuery results with pagination metadata"
// @Failure 400 {object} map[string]string "Bad request - missing or invalid parameters"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /v1/articles/search [get]
// @Example Request:  GET /v1/articles/search?q=climate%20change&size=10&lang=english
// @Example Response: {"hits": [...], "next_cursor": "eyJ...", "has_more": true, "total_matches": 1523}
func (r *SearchRouter) searchHandler(c echo.Context) error {
	// Support both 'q' (preferred) and 'query' (legacy) parameters
	query := c.QueryParam("q")
	if query == "" {
		query = c.QueryParam("query") // Backward compatibility
	}
	cursorStr := c.QueryParam("cursor")
	sizeStr := c.QueryParam("size")

	if query == "" {
		return apperr.NewValidation("q parameter is required")
	}

	sizeInt, err := r.parseSize(sizeStr)
	if err != nil {
		return err
	}

	var cursor *dquery.Cursor
	if cursorStr != "" {
		cursor, err = dquery.DecodeCursor(cursorStr)
		if err != nil {
			return apperr.NewValidation("invalid cursor parameter")
		}
	}

	queryString := dquery.NewQueryString(query)
	searchResult, err := r.searcher.SearchStringQuery(c.Request().Context(), queryString, &dquery.BaseOptions{
		Cursor: cursor,
		Size:   sizeInt,
	})
	if err != nil {
		slog.Error("Failed to execute full-text search", "error", err, "query", query)
		return err
	}

	return r.buildResponse(c, searchResult)
}

// structuredSearchHandler handles structured search requests (POST)
//
// This endpoint accepts complex, structured queries with explicit control over:
// - Which fields to search
// - Field-level weights/boosting
// - Operator logic (AND/OR)
// - Language-specific analysis
// - Fuzziness/typo tolerance
//
// Supports multiple query types via the query wrapper pattern.
// Query types: match, multi_match (more coming: bool, phrase, query_string)
//
// @Summary Structured search API
// @Description Execute structured search queries with explicit control over fields, weights, and operators. Supports match and multi_match query types. Follows Elasticsearch query DSL pattern.
// @Tags search
// @Accept json
// @Produce json
// @Param request body dto.SearchRequest true "Structured search request with query wrapper"
// @Success 200 {object} dto.SearchResponse "SearchStringQuery results with pagination metadata"
// @Failure 400 {object} map[string]string "Bad request - invalid query structure or parameters"
// @Failure 500 {object} map[string]string "Internal server error"
// @Failure 501 {object} map[string]string "Query type not supported by searcher backend"
// @Router /v1/articles/structured [post]
// @Example Match Request:
//
//	{
//	  "size": 10,
//	  "query": {
//	    "match": {
//	      "field": "title",
//	      "query": "climate change",
//	      "operator": "and",
//	      "fuzziness": "AUTO",
//	      "language": "english"
//	    }
//	  }
//	}
//
// @Example MultiMatch Request:
//
//	{
//	  "size": 10,
//	  "query": {
//	    "multi_match": {
//	      "query": "renewable energy",
//	      "fields": ["title", "description", "content"],
//	      "field_weights": {
//	        "title": 3.0,
//	        "description": 2.0,
//	        "content": 1.0
//	      },
//	      "operator": "or",
//	      "language": "english"
//	    }
//	  }
//	}
func (r *SearchRouter) structuredSearchHandler(c echo.Context) error {
	var req dto.SearchRequest
	if err := c.Bind(&req); err != nil {
		slog.Error("Failed to bind search request", "error", err)
		return apperr.NewValidation("invalid request body")
	}

	if err := c.Validate(&req); err != nil {
		return err
	}

	sizeInt := pagination.PageDefaultSize
	if req.Size > 0 {
		if req.Size > pagination.PageMaxSize {
			return apperr.NewValidation(fmt.Sprintf("size parameter exceeds maximum of %d", pagination.PageMaxSize))
		}
		sizeInt = req.Size
	}

	var cursor *dquery.Cursor
	if req.Cursor != "" {
		var err error
		cursor, err = dquery.DecodeCursor(req.Cursor)
		if err != nil {
			return apperr.NewValidation("invalid cursor parameter")
		}
	}

	opts := &dquery.BaseOptions{
		Cursor: cursor,
		Size:   sizeInt,
	}

	queryType := req.Query.GetQueryType()
	switch queryType {
	case dquery.MatchType:
		return r.handleMatchQuery(c, req.Query.Match, opts)
	case dquery.MultiMatchType:
		return r.handleMultiMatchQuery(c, req.Query.MultiMatch, opts)
	case dquery.PhraseType:
		return r.handlePhraseQuery(c, req.Query.Phrase, opts)
	case dquery.BooleanType:
		return r.handleBooleanQuery(c, req.Query.Boolean, opts)
	case dquery.HybridType:
		return r.handleHybridQuery(c, req.Query.Hybrid, opts)
	default:
		return apperr.NewValidation("query must specify one of: match, multi_match, phrase, boolean, hybrid")
	}
}

func (r *SearchRouter) handleHybridQuery(c echo.Context, params *dto.HybridParams, options *dquery.BaseOptions) error {
	if r.hybridSearcher == nil {
		return apperr.NewValidation("hybrid search is not enabled on this server")
	}

	domainQuery, err := params.ToDomain()
	if err != nil {
		return err
	}

	searchResult, err := r.hybridSearcher.SearchHybrid(c.Request().Context(), domainQuery, options)
	if err != nil {
		slog.Error("Failed to execute hybrid search", "error", err, "query", params.Query)
		return err
	}

	return r.buildResponse(c, searchResult)
}

func (r *SearchRouter) handleMatchQuery(c echo.Context, params *dto.MatchParams, options *dquery.BaseOptions) error {
	domainQuery, err := params.ToDomain()
	if err != nil {
		return err
	}

	searchResult, err := r.searcher.SearchField(c.Request().Context(), domainQuery, options)
	if err != nil {
		slog.Error("Failed to execute match search", "error", err, "field", params.Field, "query", params.Query)
		return err
	}

	return r.buildResponse(c, searchResult)
}

func (r *SearchRouter) handleMultiMatchQuery(c echo.Context, params *dto.MultiMatchParams, options *dquery.BaseOptions) error {
	domainQuery, err := params.ToDomain()
	if err != nil {
		return err
	}

	searchResult, err := r.searcher.SearchFields(c.Request().Context(), domainQuery, options)
	if err != nil {
		slog.Error("Failed to execute multi_match search", "error", err, "fields", params.Fields, "query", params.Query)
		return err
	}

	return r.buildResponse(c, searchResult)
}

func (r *SearchRouter) handlePhraseQuery(c echo.Context, params *dto.PhraseParams, options *dquery.BaseOptions) error {
	domainQuery, err := params.ToDomain()
	if err != nil {
		return err
	}

	searchResult, err := r.searcher.SearchPhrase(c.Request().Context(), domainQuery, options)
	if err != nil {
		slog.Error("Failed to execute phrase search", "error", err, "fields", params.Fields, "query", params.Query, "slop", params.Slop)
		return err
	}

	return r.buildResponse(c, searchResult)
}

func (r *SearchRouter) handleBooleanQuery(c echo.Context, params *dto.BooleanParams, options *dquery.BaseOptions) error {
	domainQuery, err := params.ToDomain()
	if err != nil {
		return err
	}

	searchResult, err := r.searcher.SearchBoolean(c.Request().Context(), domainQuery, options)
	if err != nil {
		slog.Error("Failed to execute boolean search", "error", err, "expression", params.Expression)
		return err
	}

	return r.buildResponse(c, searchResult)
}

// handleSematicQuery handles semantic query search (GET)
//
// This endpoint provides a vector-based semantic search experience using the embedding model.
// The application automatically determines optimal search strategy based on index configuration.
// Results are cacheable and URLs are bookmarkable.
//
// @Summary Vector-based semantic search
// @Description Perform vector-based semantic search using the embedding model. Supports pagination via cursor. Application determines optimal search strategy based on index configuration. Results are cacheable and bookmarkable.
// @Tags search
// @Accept json
// @Produce json
// @Param q query string true "SearchStringQuery query text" example("climate change")
// @Param size query int false "Results per page (default: 100, max: 10000)" example(10)
// @Param cursor query string false "Pagination cursor (base64-encoded from previous response)"
// @Success 200 {object} dto.SemanticSearchResponse "SearchStringQuery results with pagination metadata"
// @Failure 400 {object} map[string]string "Bad request - missing or invalid parameters"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /v1/articles/semantic_search [get]
// @Example Request:  GET /v1/articles/semantic_search?q=climate%20change&size=10
// @Example Response: {"hits": [...], "next_cursor": "eyJ...", "has_more": true}
func (r *SearchRouter) handleSematicQuery(c echo.Context) error {
	query := c.QueryParam("q")
	if query == "" {
		return apperr.NewValidation("q parameter is required")
	}

	sizeStr := c.QueryParam("size")
	size, err := r.parseSize(sizeStr)
	if err != nil {
		return err
	}

	cursorStr := c.QueryParam("cursor")

	req := dto.SemanticSearchRequest{
		Query:  query,
		Size:   size,
		Cursor: cursorStr,
	}

	var cursor *dquery.Cursor
	if req.Cursor != "" {
		var err error
		cursor, err = dquery.DecodeCursor(req.Cursor)
		if err != nil {
			return apperr.NewValidation("invalid cursor parameter")
		}
	}

	options := &dquery.BaseOptions{
		Cursor: cursor,
		Size:   size,
	}

	domainQuery, err := req.ToDomain()
	if err != nil {
		return err
	}

	searchResult, err := r.semanticSearcher.SearchSemantic(c.Request().Context(), domainQuery, options)
	if err != nil {
		slog.Error("Failed to execute semantic search", "error", err, "query", req.Query)
		return err
	}

	var nextCursorStr *string
	if searchResult.NextCursor != nil {
		encoded, err := dquery.EncodeCursor(searchResult.NextCursor.Score, searchResult.NextCursor.ID)
		if err != nil {
			slog.Error("Failed to encode cursor", "error", err)
			return fmt.Errorf("failed to encode cursor: %w", err)
		}
		nextCursorStr = &encoded
	}

	hits := searchResult.Hits
	if hits == nil {
		hits = []dto.Article{}
	}

	apiResponse := dto.SemanticSearchResponse{
		Hits:       hits,
		NextCursor: nextCursorStr,
		HasMore:    searchResult.HasMore,
	}

	return c.JSON(http.StatusOK, apiResponse)
}

func (r *SearchRouter) parseSize(sizeStr string) (int, error) {
	if sizeStr == "" {
		return pagination.PageDefaultSize, nil
	}
	sizeInt, err := strconv.Atoi(sizeStr)
	if err != nil || sizeInt < 1 {
		return 0, apperr.NewValidation("invalid size parameter")
	}
	if sizeInt > pagination.PageMaxSize {
		return 0, apperr.NewValidation(fmt.Sprintf("size parameter exceeds maximum of %d", pagination.PageMaxSize))
	}
	return sizeInt, nil
}

func (r *SearchRouter) buildResponse(c echo.Context, searchResult *storage.SearchResult) error {
	var nextCursorStr *string
	if searchResult.NextCursor != nil {
		encoded, err := dquery.EncodeCursor(searchResult.NextCursor.Score, searchResult.NextCursor.ID)
		if err != nil {
			slog.Error("Failed to encode cursor", "error", err)
			return fmt.Errorf("failed to encode cursor: %w", err)
		}
		nextCursorStr = &encoded
	}

	hits := searchResult.Hits
	if hits == nil {
		hits = []dto.ArticleSearchResult{}
	}

	apiResponse := dto.SearchResponse{
		Hits:         hits,
		NextCursor:   nextCursorStr,
		HasMore:      searchResult.HasMore,
		MaxScore:     searchResult.MaxScore,
		PageMaxScore: searchResult.PageMaxScore,
		TotalMatches: searchResult.TotalMatches,
	}

	return c.JSON(http.StatusOK, apiResponse)
}
