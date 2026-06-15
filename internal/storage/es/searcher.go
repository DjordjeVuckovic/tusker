package es

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/DjordjeVuckovic/tusker/internal/api/dto"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/token"
	dquery "github.com/DjordjeVuckovic/tusker/internal/types/query"
	"github.com/DjordjeVuckovic/tusker/pkg/utils"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/operator"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/sortorder"
	"github.com/google/uuid"
)

type Searcher struct {
	client    *elasticsearch.TypedClient
	indexName string
	tokenizer *token.BoolTokenizer
}

func NewSearcher(config ClientConfig) (*Searcher, error) {
	client, err := newClient(config)

	if err != nil {
		return nil, fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	return &Searcher{
		client:    client,
		indexName: config.IndexName,
		tokenizer: token.NewBoolTokenizer(),
	}, nil
}

// SearchStringQuery implements the storage.FtsSearcher interface.
// Performs simple string-based search using Elasticsearch's multi_match query with BM25
// Application determines optimal fields and weights based on index configuration
func (r *Searcher) SearchStringQuery(ctx context.Context, query *dquery.String, baseOpts *dquery.BaseOptions) (*storage.SearchResult, error) {
	// Use default fields with default weights (application-determined)
	cursor, size := baseOpts.Cursor, baseOpts.Size
	fields := dquery.DefaultFields
	fieldWeights := dquery.DefaultFieldWeights
	queryOperator := query.GetDefaultOperator()

	slog.Info("Executing es query_string search",
		"query", query.Query,
		"language", query.GetLanguage(),
		"fields", fields,
		"operator", queryOperator,
		"has_cursor", cursor != nil,
		"size", size)

	// Build field list with boosting from application defaults
	// Format: "title^1.0", "description^1.0", "content^1.0"
	fieldsWithBoost := make([]string, 0, len(fields))
	for _, field := range fields {
		weight := fieldWeights[field]
		if weight != 1.0 {
			fieldsWithBoost = append(fieldsWithBoost, fmt.Sprintf("%s^%.1f", field, weight))
		} else {
			fieldsWithBoost = append(fieldsWithBoost, field)
		}
	}

	// Build multi_match query
	multiMatch := &types.MultiMatchQuery{
		Query:  query.Query,
		Fields: fieldsWithBoost,
	}

	// Set operator (AND/OR)
	if queryOperator == "and" {
		and := operator.And
		multiMatch.Operator = &and
	} else {
		or := operator.Or
		multiMatch.Operator = &or
	}

	slog.Debug("Elasticsearch multi_match query",
		"fields_with_boost", fieldsWithBoost,
		"operator", queryOperator)

	searchReq := r.client.Search().
		Index(r.indexName).
		Query(&types.Query{
			MultiMatch: multiMatch,
		}).
		Size(size + 1).
		TrackScores(true)

	if cursor != nil {
		searchReq = searchReq.SearchAfter(
			types.FieldValue(cursor.Score),
			types.FieldValue(cursor.ID.String()),
		)
	}

	sortOrderDesc := sortorder.Desc
	searchReq = searchReq.Sort(
		&types.SortOptions{
			SortOptions: map[string]types.FieldSort{
				"_score": {Order: &sortOrderDesc},
			},
		},
		&types.SortOptions{
			SortOptions: map[string]types.FieldSort{
				"id": {Order: &sortOrderDesc},
			},
		},
	)

	var err error

	res, err := searchReq.Do(ctx)
	if err != nil {
		slog.Error("Elasticsearch query failed", "error", err, "query", query.Query, "cursor", cursor != nil)
		return nil, fmt.Errorf("failed to execute search: %w", err)
	}

	maxScore := dquery.CalcSafeScore((*float64)(res.Hits.MaxScore))

	articles, rawScores, err := r.mapToResult(res.Hits.Hits, maxScore)
	if err != nil {
		return nil, fmt.Errorf("failed to map search results to types: %w", err)
	}

	slog.Info("Es search results fetched",
		"total_matches", res.Hits.Total.Value,
		"returned_count", len(articles),
		"max_score", *res.Hits.MaxScore,
		"normalized_max", maxScore)

	hasMore := len(articles) > size
	if hasMore {
		articles = articles[:size]
		rawScores = rawScores[:size]
	}

	var nextCursor *dquery.Cursor
	if hasMore && len(articles) > 0 {
		nextCursor = &dquery.Cursor{
			Score: rawScores[len(rawScores)-1],
			ID:    articles[len(articles)-1].Article.ID,
		}
	}

	return &storage.SearchResult{
		Hits:         articles,
		NextCursor:   nextCursor,
		HasMore:      hasMore,
		MaxScore:     utils.RoundFloat64(float64(*res.Hits.MaxScore), dquery.ScoreDecimalPlaces),
		PageMaxScore: utils.RoundFloat64(rawScores[0], dquery.ScoreDecimalPlaces),
		TotalMatches: res.Hits.Total.Value,
	}, nil
}

func (r *Searcher) mapToResult(hits []types.Hit, maxScore float64) ([]dto.ArticleSearchResult, []float64, error) {
	if hits == nil {
		return make([]dto.ArticleSearchResult, 0), make([]float64, 0), nil
	}

	var articles []dto.ArticleSearchResult
	var rawScores []float64

	for _, hit := range hits {
		var doc ArticleDocument
		if err := json.Unmarshal(hit.Source_, &doc); err != nil {
			return nil, nil, fmt.Errorf("failed to unmarshal document: %w", err)
		}

		article := dto.Article{
			ID:          uuid.MustParse(doc.ID),
			Title:       doc.Title,
			Subtitle:    doc.Subtitle,
			Content:     doc.Content,
			Author:      doc.Author,
			Description: doc.Description,
			URL:         doc.URL,
			Language:    doc.Language,
			CreatedAt:   doc.CreatedAt,
			Metadata: dto.ArticleMetadata{
				SourceId:    doc.SourceId,
				SourceName:  doc.SourceName,
				PublishedAt: doc.PublishedAt,
				Category:    doc.Category,
				ImportedAt:  doc.ImportedAt,
			},
		}

		rawScore := float64(*hit.Score_)
		normalizedRank := rawScore / maxScore

		searchResult := dto.ArticleSearchResult{
			Article:         article,
			ScoreNormalized: normalizedRank,
			Score:           float64(*hit.Score_),
		}

		articles = append(articles, searchResult)
		rawScores = append(rawScores, rawScore)
	}

	return articles, rawScores, nil
}

// SearchField implements storage.SingleMatchSearcher interface
// Performs single-field match query using Elasticsearch's match query
func (r *Searcher) SearchField(ctx context.Context, query *dquery.Match, baseOpts *dquery.BaseOptions) (*storage.SearchResult, error) {
	cursor, size := baseOpts.Cursor, baseOpts.Size
	slog.Info("Executing es match search",
		"query", query.Query,
		"field", query.Field,
		"operator", query.GetOperator(),
		"fuzziness", query.Fuzziness,
		"has_cursor", cursor != nil,
		"size", size)

	// Build single-field match query
	matchQuery := &types.MatchQuery{
		Query: query.Query,
	}

	// Set operator using value object
	if query.GetOperator().IsAnd() {
		and := operator.And
		matchQuery.Operator = &and
	} else {
		or := operator.Or
		matchQuery.Operator = &or
	}

	// Set fuzziness if specified
	if query.Fuzziness != "" {
		matchQuery.Fuzziness = &query.Fuzziness
	}

	slog.Debug("Elasticsearch match query",
		"field", query.Field,
		"operator", query.GetOperator(),
		"fuzziness", query.Fuzziness)

	// Build search request with match query on specific field
	searchReq := r.client.Search().
		Index(r.indexName).
		Query(&types.Query{
			Match: map[string]types.MatchQuery{
				query.Field: *matchQuery,
			},
		}).
		Size(size + 1).
		TrackScores(true)

	// Add cursor support and sorting
	if cursor != nil {
		searchReq = searchReq.SearchAfter(
			types.FieldValue(cursor.Score),
			types.FieldValue(cursor.ID.String()),
		)
	}

	sortOrderDesc := sortorder.Desc
	searchReq = searchReq.Sort(
		&types.SortOptions{
			SortOptions: map[string]types.FieldSort{
				"_score": {Order: &sortOrderDesc},
			},
		},
		&types.SortOptions{
			SortOptions: map[string]types.FieldSort{
				"id": {Order: &sortOrderDesc},
			},
		},
	)

	// Execute query
	res, err := searchReq.Do(ctx)
	if err != nil {
		slog.Error("Elasticsearch match query failed", "error", err, "query", query.Query, "field", query.Field)
		return nil, fmt.Errorf("failed to execute match search: %w", err)
	}

	maxScore := dquery.CalcSafeScore((*float64)(res.Hits.MaxScore))

	articles, rawScores, err := r.mapToResult(res.Hits.Hits, maxScore)
	if err != nil {
		return nil, fmt.Errorf("failed to map search results to types: %w", err)
	}

	slog.Info("ES match search results fetched",
		"total_matches", res.Hits.Total.Value,
		"returned_count", len(articles),
		"max_score", *res.Hits.MaxScore,
		"normalized_max", maxScore)

	hasMore := len(articles) > size
	if hasMore {
		articles = articles[:size]
		rawScores = rawScores[:size]
	}

	var nextCursor *dquery.Cursor
	if hasMore && len(articles) > 0 {
		nextCursor = &dquery.Cursor{
			Score: rawScores[len(rawScores)-1],
			ID:    articles[len(articles)-1].Article.ID,
		}
	}

	return &storage.SearchResult{
		Hits:         articles,
		NextCursor:   nextCursor,
		HasMore:      hasMore,
		MaxScore:     utils.RoundFloat64(float64(*res.Hits.MaxScore), dquery.ScoreDecimalPlaces),
		PageMaxScore: utils.RoundFloat64(rawScores[0], dquery.ScoreDecimalPlaces),
		TotalMatches: res.Hits.Total.Value,
	}, nil
}

// SearchFields implements storage.MultiMatchSearcher interface
// Performs multi-field match query using Elasticsearch's multi_match query
func (r *Searcher) SearchFields(ctx context.Context, query *dquery.MultiMatch, baseOpts *dquery.BaseOptions) (*storage.SearchResult, error) {
	cursor, size := baseOpts.Cursor, baseOpts.Size
	slog.Info("Executing es multi_match search",
		"query", query.Query,
		"fields", query.Fields,
		"operator", query.GetOperator(),
		"has_cursor", cursor != nil,
		"size", size)

	// Extract query parameters
	fields := query.GetFields()
	queryOperator := query.GetOperator()

	// Build field list with boosting
	fieldsWithWeight := make([]string, 0, len(fields))
	for _, field := range fields {
		if field.Weight != 1.0 {
			fieldsWithWeight = append(fieldsWithWeight, fmt.Sprintf("%s^%.1f", field.Name, field.Weight))
		} else {
			fieldsWithWeight = append(fieldsWithWeight, field.Name)
		}
	}

	// Build multi_match query
	multiMatch := &types.MultiMatchQuery{
		Query:  query.Query,
		Fields: fieldsWithWeight,
	}

	// Set operator using value object
	if queryOperator.IsAnd() {
		and := operator.And
		multiMatch.Operator = &and
	} else {
		or := operator.Or
		multiMatch.Operator = &or
	}

	// Build and execute search request
	searchReq := r.client.Search().
		Index(r.indexName).
		Query(&types.Query{
			MultiMatch: multiMatch,
		}).
		Size(size + 1).
		TrackScores(true)

	// Add cursor support and sorting
	if cursor != nil {
		searchReq = searchReq.SearchAfter(
			types.FieldValue(cursor.Score),
			types.FieldValue(cursor.ID.String()),
		)
	}

	sortOrderDesc := sortorder.Desc
	searchReq = searchReq.Sort(
		&types.SortOptions{
			SortOptions: map[string]types.FieldSort{
				"_score": {Order: &sortOrderDesc},
			},
		},
		&types.SortOptions{
			SortOptions: map[string]types.FieldSort{
				"id": {Order: &sortOrderDesc},
			},
		},
	)

	// Execute query
	res, err := searchReq.Do(ctx)
	if err != nil {
		slog.Error("Elasticsearch multi_match query failed", "error", err, "query", query.Query)
		return nil, fmt.Errorf("failed to execute multi_match search: %w", err)
	}

	maxScore := dquery.CalcSafeScore((*float64)(res.Hits.MaxScore))

	articles, rawScores, err := r.mapToResult(res.Hits.Hits, maxScore)
	if err != nil {
		return nil, fmt.Errorf("failed to map search results to types: %w", err)
	}

	slog.Info("ES multi_match search results fetched",
		"total_matches", res.Hits.Total.Value,
		"returned_count", len(articles),
		"max_score", *res.Hits.MaxScore,
		"normalized_max", maxScore)

	hasMore := len(articles) > size
	if hasMore {
		articles = articles[:size]
		rawScores = rawScores[:size]
	}

	var nextCursor *dquery.Cursor
	if hasMore && len(articles) > 0 {
		nextCursor = &dquery.Cursor{
			Score: rawScores[len(rawScores)-1],
			ID:    articles[len(articles)-1].Article.ID,
		}
	}

	return &storage.SearchResult{
		Hits:         articles,
		NextCursor:   nextCursor,
		HasMore:      hasMore,
		MaxScore:     utils.RoundFloat64(float64(*res.Hits.MaxScore), dquery.ScoreDecimalPlaces),
		PageMaxScore: utils.RoundFloat64(rawScores[0], dquery.ScoreDecimalPlaces),
		TotalMatches: res.Hits.Total.Value,
	}, nil
}

// SearchPhrase implements storage.FtsSearcher interface
// Performs phrase search using Elasticsearch's match_phrase query with slop support
func (r *Searcher) SearchPhrase(ctx context.Context, query *dquery.Phrase, baseOpts *dquery.BaseOptions) (*storage.SearchResult, error) {
	cursor, size := baseOpts.Cursor, baseOpts.Size
	slop := query.GetSlop()

	slog.Info("Executing es phrase search",
		"query", query.Query,
		"fields", query.Fields,
		"slop", slop,
		"language", query.GetLanguage(),
		"has_cursor", cursor != nil,
		"size", size)

	// Build bool query with should clauses for each field
	// Each field gets a match_phrase query with the same slop
	shouldClauses := make([]types.Query, 0, len(query.Fields))
	for _, field := range query.Fields {
		matchPhraseQuery := types.MatchPhraseQuery{
			Query: query.Query,
		}
		if slop > 0 {
			matchPhraseQuery.Slop = &slop
		}

		shouldClauses = append(shouldClauses, types.Query{
			MatchPhrase: map[string]types.MatchPhraseQuery{
				field: matchPhraseQuery,
			},
		})
	}

	// Build bool query - at least one field should match
	boolQuery := &types.BoolQuery{
		Should:             shouldClauses,
		MinimumShouldMatch: "1",
	}

	slog.Debug("Elasticsearch phrase query",
		"fields", query.Fields,
		"slop", slop,
		"num_should_clauses", len(shouldClauses))

	// Build search request
	searchReq := r.client.Search().
		Index(r.indexName).
		Query(&types.Query{
			Bool: boolQuery,
		}).
		Size(size + 1).
		TrackScores(true)

	// Add cursor support
	if cursor != nil {
		searchReq = searchReq.SearchAfter(
			types.FieldValue(cursor.Score),
			types.FieldValue(cursor.ID.String()),
		)
	}

	// Add sorting
	sortOrderDesc := sortorder.Desc
	searchReq = searchReq.Sort(
		&types.SortOptions{
			SortOptions: map[string]types.FieldSort{
				"_score": {Order: &sortOrderDesc},
			},
		},
		&types.SortOptions{
			SortOptions: map[string]types.FieldSort{
				"id": {Order: &sortOrderDesc},
			},
		},
	)

	// Execute query
	res, err := searchReq.Do(ctx)
	if err != nil {
		slog.Error("Elasticsearch phrase query failed", "error", err, "query", query.Query, "fields", query.Fields)
		return nil, fmt.Errorf("failed to execute phrase search: %w", err)
	}

	maxScore := dquery.CalcSafeScore((*float64)(res.Hits.MaxScore))

	articles, rawScores, err := r.mapToResult(res.Hits.Hits, maxScore)
	if err != nil {
		return nil, fmt.Errorf("failed to map search results to types: %w", err)
	}

	slog.Info("ES phrase search results fetched",
		"total_matches", res.Hits.Total.Value,
		"returned_count", len(articles),
		"max_score", res.Hits.MaxScore,
		"normalized_max", maxScore)

	hasMore := len(articles) > size
	if hasMore {
		articles = articles[:size]
		rawScores = rawScores[:size]
	}

	var nextCursor *dquery.Cursor
	if hasMore && len(articles) > 0 {
		nextCursor = &dquery.Cursor{
			Score: rawScores[len(rawScores)-1],
			ID:    articles[len(articles)-1].Article.ID,
		}
	}

	// Handle case where no results found
	var maxScoreValue float64
	var pageMaxScore float64
	if res.Hits.MaxScore != nil {
		maxScoreValue = utils.RoundFloat64(float64(*res.Hits.MaxScore), dquery.ScoreDecimalPlaces)
	}
	if len(rawScores) > 0 {
		pageMaxScore = utils.RoundFloat64(rawScores[0], dquery.ScoreDecimalPlaces)
	}

	return &storage.SearchResult{
		Hits:         articles,
		NextCursor:   nextCursor,
		HasMore:      hasMore,
		MaxScore:     maxScoreValue,
		PageMaxScore: pageMaxScore,
		TotalMatches: res.Hits.Total.Value,
	}, nil
}

func (r *Searcher) SearchBoolean(ctx context.Context, query *dquery.Boolean, baseOpts *dquery.BaseOptions) (*storage.SearchResult, error) {
	cursor, size := baseOpts.Cursor, baseOpts.Size

	slog.Info("Executing es boolean search",
		"expression", query.Expression,
		"has_cursor", cursor != nil,
		"size", size)

	tokens := r.tokenizer.Tokenize(query.Expression)
	if err := r.tokenizer.Validate(tokens); err != nil {
		slog.Error("Invalid boolean query expression", "error", err, "expression", query.Expression)
		return nil, fmt.Errorf("invalid boolean query expression: %w", err)
	}

	fields := make([]string, 0, len(dquery.RecommendedFieldWeights))
	for field, weight := range dquery.RecommendedFieldWeights {
		if weight != 1.0 {
			fields = append(fields, fmt.Sprintf("%s^%.1f", field, weight))
		} else {
			fields = append(fields, field)
		}
	}

	queryStringQuery := &types.QueryStringQuery{
		Query:  query.Expression,
		Fields: fields,
	}

	searchReq := r.client.Search().
		Index(r.indexName).
		Query(&types.Query{
			QueryString: queryStringQuery,
		}).
		Size(size + 1).
		TrackScores(true)

	if cursor != nil {
		searchReq = searchReq.SearchAfter(
			types.FieldValue(cursor.Score),
			types.FieldValue(cursor.ID.String()),
		)
	}

	sortOrderDesc := sortorder.Desc
	searchReq = searchReq.Sort(
		&types.SortOptions{
			SortOptions: map[string]types.FieldSort{
				"_score": {Order: &sortOrderDesc},
			},
		},
		&types.SortOptions{
			SortOptions: map[string]types.FieldSort{
				"id": {Order: &sortOrderDesc},
			},
		},
	)

	res, err := searchReq.Do(ctx)
	if err != nil {
		slog.Error("Elasticsearch boolean query failed", "error", err, "expression", query.Expression)
		return nil, fmt.Errorf("failed to execute boolean search: %w", err)
	}

	maxScore := dquery.CalcSafeScore((*float64)(res.Hits.MaxScore))

	articles, rawScores, err := r.mapToResult(res.Hits.Hits, maxScore)
	if err != nil {
		return nil, fmt.Errorf("failed to map search results to types: %w", err)
	}

	slog.Info("ES boolean search results fetched",
		"total_matches", res.Hits.Total.Value,
		"returned_count", len(articles),
		"max_score", res.Hits.MaxScore,
		"normalized_max", maxScore)

	hasMore := len(articles) > size
	if hasMore {
		articles = articles[:size]
		rawScores = rawScores[:size]
	}

	var nextCursor *dquery.Cursor
	if hasMore && len(articles) > 0 {
		nextCursor = &dquery.Cursor{
			Score: rawScores[len(rawScores)-1],
			ID:    articles[len(articles)-1].Article.ID,
		}
	}

	var maxScoreValue float64
	var pageMaxScore float64
	if res.Hits.MaxScore != nil {
		maxScoreValue = utils.RoundFloat64(float64(*res.Hits.MaxScore), dquery.ScoreDecimalPlaces)
	}
	if len(rawScores) > 0 {
		pageMaxScore = utils.RoundFloat64(rawScores[0], dquery.ScoreDecimalPlaces)
	}

	return &storage.SearchResult{
		Hits:         articles,
		NextCursor:   nextCursor,
		HasMore:      hasMore,
		MaxScore:     maxScoreValue,
		PageMaxScore: pageMaxScore,
		TotalMatches: res.Hits.Total.Value,
	}, nil
}

// Compile-time interface assertions
var _ storage.FtsSearcher = (*Searcher)(nil)
