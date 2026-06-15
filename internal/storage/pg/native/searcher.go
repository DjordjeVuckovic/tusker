package native

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/DjordjeVuckovic/tusker/internal/api/dto"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/storage/pg"
	dquery "github.com/DjordjeVuckovic/tusker/internal/types/query"
	"github.com/DjordjeVuckovic/tusker/pkg/utils"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Searcher struct {
	db *pgxpool.Pool
}

func NewReader(pool *pg.ConnectionPool) (*Searcher, error) {
	return &Searcher{db: pool.GetConn()}, nil
}

// SearchStringQuery implements storage.FtsSearcher interface
// Performs simple string-based search using PostgreSQL's tsvector and plainto_tsquery
// Application determines optimal fields and weights based on index configuration
func (r *Searcher) SearchStringQuery(ctx context.Context, query *dquery.String, baseOpts *dquery.BaseOptions) (*storage.SearchResult, error) {
	cursor, size := baseOpts.Cursor, baseOpts.Size
	slog.Info("Executing pool query_string search", "query", query.Query, "has_cursor", cursor != nil, "size", size)

	var globalMaxScore float64
	var count int64
	maxSQL := `
			SELECT COALESCE(MAX(ts_rank(search_vector, plainto_tsquery('english', $1))), 0.0) as max_score, COUNT(*)
			FROM articles
			WHERE search_vector @@ plainto_tsquery('english', $1)
		`
	if err := r.db.QueryRow(ctx, maxSQL, query.Query).Scan(&globalMaxScore, &count); err != nil {
		slog.Error("Failed to fetch global max score", "error", err)
		return nil, fmt.Errorf("cannot fetch global max score: %w", err)
	}
	if count == 0 {
		return &storage.SearchResult{}, nil
	}
	slog.Info("Computed global max score", "max_score", globalMaxScore, "total_matches", count)

	var searchSQL string
	var args []interface{}

	if cursor == nil {
		searchSQL = `
			SELECT
				id, title, subtitle, content, author, description, url, language, created_at, metadata,
				ts_rank(search_vector, plainto_tsquery('english', $1)) as rank
			FROM articles
			WHERE search_vector @@ plainto_tsquery('english', $1)
			ORDER BY rank DESC, id DESC
			LIMIT $2
		`
		args = []interface{}{query.Query, size + 1}
	} else {
		searchSQL = `
			SELECT
				id, title, subtitle, content, author, description, url, language, created_at, metadata,
				ts_rank(search_vector, plainto_tsquery('english', $1)) as rank
			FROM articles
			WHERE search_vector @@ plainto_tsquery('english', $1)
			  AND (ts_rank(search_vector, plainto_tsquery('english', $1)), id) < ($2, $3)
			ORDER BY rank DESC, id DESC
			LIMIT $4
		`
		args = []interface{}{query.Query, cursor.Score, cursor.ID, size + 1}
	}

	rows, err := r.db.Query(ctx, searchSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute search query: %w", err)
	}
	defer rows.Close()

	var articles []dto.ArticleSearchResult
	var rawScores []float64

	for rows.Next() {
		var metadataJSON []byte
		var rawScore float64
		var article dto.Article

		if err := rows.Scan(
			&article.ID,
			&article.Title,
			&article.Subtitle,
			&article.Content,
			&article.Author,
			&article.Description,
			&article.URL,
			&article.Language,
			&article.CreatedAt,
			&metadataJSON,
			&rawScore,
		); err != nil {
			return nil, fmt.Errorf("failed to scan article: %w", err)
		}

		if err := json.Unmarshal(metadataJSON, &article.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}

		searchResult := dto.ArticleSearchResult{
			Article:         article,
			Score:           utils.RoundFloat64(rawScore, dquery.ScoreDecimalPlaces),
			ScoreNormalized: utils.RoundFloat64(rawScore/globalMaxScore, dquery.ScoreDecimalPlaces),
		}

		articles = append(articles, searchResult)
		rawScores = append(rawScores, rawScore)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	slog.Info("PG search results fetched",
		"total_page_matches", len(articles),
		"global_max_score", globalMaxScore)

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
		MaxScore:     utils.RoundFloat64(globalMaxScore, dquery.ScoreDecimalPlaces),
		PageMaxScore: utils.RoundFloat64(rawScores[0], dquery.ScoreDecimalPlaces),
		TotalMatches: count,
	}, nil
}

// SearchField implements storage.SingleMatchSearcher interface
// Performs single-field match query using PostgreSQL's tsvector
func (r *Searcher) SearchField(ctx context.Context, query *dquery.Match, baseOpts *dquery.BaseOptions) (*storage.SearchResult, error) {
	cursor, size := baseOpts.Cursor, baseOpts.Size
	slog.Info("Executing pool match search",
		"query", query.Query,
		"field", query.Field,
		"operator", query.GetOperator(),
		"language", query.GetLanguage(),
		"has_cursor", cursor != nil,
		"size", size)

	lang := query.GetLanguage()
	operator := query.GetOperator()

	// Build FieldWeight for single field
	fieldBoosts := []FieldWeight{{Field: query.Field, Weight: 1.0}}
	whereClause := buildTsWhereClause(fieldBoosts, lang, operator, 1)
	rankExpr := buildRankExpression(fieldBoosts, lang, operator, 1)

	slog.Debug("PostgreSQL match query components",
		"where", whereClause,
		"rank", rankExpr)

	// Get global max score and total count
	var globalMaxScore float64
	var count int64
	maxSQL := fmt.Sprintf(`
		SELECT COALESCE(MAX(%s), 0.0) as max_score, COUNT(*)
		FROM articles
		WHERE %s
	`, rankExpr, whereClause)

	if err := r.db.QueryRow(ctx, maxSQL, query.Query).Scan(&globalMaxScore, &count); err != nil {
		slog.Error("Failed to fetch global max score", "error", err)
		return nil, fmt.Errorf("cannot fetch global max score: %w", err)
	}
	if count == 0 {
		return &storage.SearchResult{}, nil
	}
	slog.Info("Computed global max score", "max_score", globalMaxScore, "total_matches", count)

	var searchSQL string
	var args []interface{}

	if cursor == nil {
		searchSQL = fmt.Sprintf(`
			SELECT
				id, title, subtitle, content, author, description, url, language, created_at, metadata,
				%s as rank
			FROM articles
			WHERE %s
			ORDER BY rank DESC, id DESC
			LIMIT $2
		`, rankExpr, whereClause)
		args = []interface{}{query.Query, size + 1}
	} else {
		searchSQL = fmt.Sprintf(`
			SELECT
				id, title, subtitle, content, author, description, url, language, created_at, metadata,
				%s as rank
			FROM articles
			WHERE %s
			  AND (%s, id) < ($2, $3)
			ORDER BY rank DESC, id DESC
			LIMIT $4
		`, rankExpr, whereClause, rankExpr)
		args = []interface{}{query.Query, cursor.Score, cursor.ID, size + 1}
	}

	rows, err := r.db.Query(ctx, searchSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute match search query: %w", err)
	}
	defer rows.Close()

	var articles []dto.ArticleSearchResult
	var rawScores []float64

	for rows.Next() {
		var metadataJSON []byte
		var rawScore float64
		var article dto.Article

		if err := rows.Scan(
			&article.ID,
			&article.Title,
			&article.Subtitle,
			&article.Content,
			&article.Author,
			&article.Description,
			&article.URL,
			&article.Language,
			&article.CreatedAt,
			&metadataJSON,
			&rawScore,
		); err != nil {
			return nil, fmt.Errorf("failed to scan article: %w", err)
		}

		if err := json.Unmarshal(metadataJSON, &article.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}

		searchResult := dto.ArticleSearchResult{
			Article:         article,
			Score:           utils.RoundFloat64(rawScore, dquery.ScoreDecimalPlaces),
			ScoreNormalized: utils.RoundFloat64(rawScore/globalMaxScore, dquery.ScoreDecimalPlaces),
		}

		articles = append(articles, searchResult)
		rawScores = append(rawScores, rawScore)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	slog.Info("PG match search results fetched",
		"total_page_matches", len(articles),
		"global_max_score", globalMaxScore)

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
		MaxScore:     utils.RoundFloat64(globalMaxScore, dquery.ScoreDecimalPlaces),
		PageMaxScore: utils.RoundFloat64(rawScores[0], dquery.ScoreDecimalPlaces),
		TotalMatches: count,
	}, nil
}

// SearchFields implements storage.MultiMatchSearcher interface
// Performs multi-field match query using PostgreSQL's weighted tsvector
func (r *Searcher) SearchFields(ctx context.Context, query *dquery.MultiMatch, baseOpts *dquery.BaseOptions) (*storage.SearchResult, error) {
	cursor, size := baseOpts.Cursor, baseOpts.Size
	lang := query.GetLanguage()
	operator := query.GetOperator()

	// Convert Fields (MultiMatchField) to FieldWeight
	fieldBoosts := make([]FieldWeight, 0, len(query.Fields))
	for _, f := range query.Fields {
		fieldBoosts = append(fieldBoosts, FieldWeight{
			Field:  f.Name,
			Weight: f.Weight,
		})
	}

	slog.Info("Executing pool multi_match search",
		"query", query.Query,
		"fields", query.Fields,
		"operator", operator,
		"language", lang,
		"has_cursor", cursor != nil,
		"size", size)

	// Use helper functions with FieldWeight
	whereClause := buildTsWhereClause(fieldBoosts, lang, operator, 1)
	rankExpr := buildRankExpression(fieldBoosts, lang, operator, 1)

	slog.Info("PostgreSQL multi_match query components",
		"where_clause", whereClause,
		"rank_expression", rankExpr,
		"field_boosts", fieldBoosts,
		"num_fields", len(fieldBoosts))

	// Get global max score and total count
	var globalMaxScore float64
	var count int64
	maxSQL := fmt.Sprintf(`
		SELECT COALESCE(MAX(%s), 0.0) as max_score, COUNT(*)
		FROM articles
		WHERE %s
	`, rankExpr, whereClause)

	if err := r.db.QueryRow(ctx, maxSQL, query.Query).Scan(&globalMaxScore, &count); err != nil {
		slog.Error("Failed to fetch global max score", "error", err)
		return nil, fmt.Errorf("cannot fetch global max score: %w", err)
	}
	if count == 0 {
		return &storage.SearchResult{}, nil
	}
	slog.Info("Computed global max score", "max_score", globalMaxScore, "total_matches", count)

	var searchSQL string
	var args []interface{}

	if cursor == nil {
		searchSQL = fmt.Sprintf(`
			SELECT
				id, title, subtitle, content, author, description, url, language, created_at, metadata,
				%s as rank
			FROM articles
			WHERE %s
			ORDER BY rank DESC, id DESC
			LIMIT $2
		`, rankExpr, whereClause)
		args = []interface{}{query.Query, size + 1}
	} else {
		searchSQL = fmt.Sprintf(`
			SELECT
				id, title, subtitle, content, author, description, url, language, created_at, metadata,
				%s as rank
			FROM articles
			WHERE %s
			  AND (%s, id) < ($2, $3)
			ORDER BY rank DESC, id DESC
			LIMIT $4
		`, rankExpr, whereClause, rankExpr)
		args = []interface{}{query.Query, cursor.Score, cursor.ID, size + 1}
	}

	rows, err := r.db.Query(ctx, searchSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute multi_match search query: %w", err)
	}
	defer rows.Close()

	var articles []dto.ArticleSearchResult
	var rawScores []float64

	for rows.Next() {
		var metadataJSON []byte
		var rawScore float64
		var article dto.Article

		if err := rows.Scan(
			&article.ID,
			&article.Title,
			&article.Subtitle,
			&article.Content,
			&article.Author,
			&article.Description,
			&article.URL,
			&article.Language,
			&article.CreatedAt,
			&metadataJSON,
			&rawScore,
		); err != nil {
			return nil, fmt.Errorf("failed to scan article: %w", err)
		}

		if err := json.Unmarshal(metadataJSON, &article.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}

		searchResult := dto.ArticleSearchResult{
			Article:         article,
			Score:           utils.RoundFloat64(rawScore, dquery.ScoreDecimalPlaces),
			ScoreNormalized: utils.RoundFloat64(rawScore/globalMaxScore, dquery.ScoreDecimalPlaces),
		}

		articles = append(articles, searchResult)
		rawScores = append(rawScores, rawScore)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	slog.Info("PG multi_match search results fetched",
		"total_page_matches", len(articles),
		"global_max_score", globalMaxScore)

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
		MaxScore:     utils.RoundFloat64(globalMaxScore, dquery.ScoreDecimalPlaces),
		PageMaxScore: utils.RoundFloat64(rawScores[0], dquery.ScoreDecimalPlaces),
		TotalMatches: count,
	}, nil
}

// SearchPhrase implements storage.FtsSearcher interface
// Performs phrase search with optional slop using PostgreSQL's phraseto_tsquery or to_tsquery
func (r *Searcher) SearchPhrase(ctx context.Context, query *dquery.Phrase, baseOpts *dquery.BaseOptions) (*storage.SearchResult, error) {
	cursor, size := baseOpts.Cursor, baseOpts.Size
	lang := query.GetLanguage()
	slop := query.GetSlop()

	slog.Info("Executing pool phrase search",
		"query", query.Query,
		"fields", query.Fields,
		"slop", slop,
		"language", lang,
		"has_cursor", cursor != nil,
		"size", size)

	// Build the phrase tsquery based on slop
	var phraseQueryExpr string
	var whereClause string
	var rankExpr string
	labels := buildWeightLabels(query.Fields)

	if slop == 0 {
		// Exact phrase matching using phraseto_tsquery
		phraseQueryExpr = fmt.Sprintf("phraseto_tsquery('%s'::regconfig, $1)", lang)

		if labels != "" {
			whereClause = fmt.Sprintf("search_vector @@ (%s::text || ':%s')::tsquery", phraseQueryExpr, labels)
		} else {
			whereClause = fmt.Sprintf("search_vector @@ %s", phraseQueryExpr)
		}
		rankExpr = fmt.Sprintf("ts_rank(search_vector, %s)", phraseQueryExpr)
	} else {
		// Slop > 0: Build OR query with distance operators
		// First, get the lexemes from the phrase using plainto_tsquery
		var lexemesStr string
		lexemeSQL := fmt.Sprintf("SELECT plainto_tsquery('%s'::regconfig, $1)::text", lang)
		if err := r.db.QueryRow(ctx, lexemeSQL, query.Query).Scan(&lexemesStr); err != nil {
			slog.Error("Failed to tokenize phrase", "error", err)
			return nil, fmt.Errorf("failed to tokenize phrase: %w", err)
		}

		// Extract lexemes from the tsquery string (e.g., "'climat' & 'chang'" -> ["climat", "chang"])
		lexemes := extractLexemesFromTsquery(lexemesStr)

		if len(lexemes) < 2 {
			// Single word or empty - fall back to simple phrase query
			phraseQueryExpr = fmt.Sprintf("phraseto_tsquery('%s'::regconfig, $1)", lang)
		} else {
			// Build slop query: term1 <-> term2 | term1 <2> term2 | term1 <3> term2 ...
			slopQueryStr := buildPhraseSlopQuery(lexemes, slop)
			phraseQueryExpr = fmt.Sprintf("to_tsquery('%s'::regconfig, '%s')", lang, slopQueryStr)
		}

		if labels != "" {
			whereClause = fmt.Sprintf("search_vector @@ (%s::text || ':%s')::tsquery", phraseQueryExpr, labels)
		} else {
			whereClause = fmt.Sprintf("search_vector @@ %s", phraseQueryExpr)
		}
		rankExpr = fmt.Sprintf("ts_rank(search_vector, %s)", phraseQueryExpr)
	}

	slog.Debug("PostgreSQL phrase query components",
		"where", whereClause,
		"rank", rankExpr,
		"slop", slop)

	// Get global max score and total count
	var globalMaxScore float64
	var count int64
	var maxSQL string
	var maxArgs []interface{}

	if slop == 0 {
		maxSQL = fmt.Sprintf(`
			SELECT COALESCE(MAX(%s), 0.0) as max_score, COUNT(*)
			FROM articles
			WHERE %s
		`, rankExpr, whereClause)
		maxArgs = []interface{}{query.Query}
	} else {
		// For slop>0, query is already embedded in the expression
		maxSQL = fmt.Sprintf(`
			SELECT COALESCE(MAX(%s), 0.0) as max_score, COUNT(*)
			FROM articles
			WHERE %s
		`, rankExpr, whereClause)
		maxArgs = []interface{}{}
	}

	if err := r.db.QueryRow(ctx, maxSQL, maxArgs...).Scan(&globalMaxScore, &count); err != nil {
		slog.Error("Failed to fetch global max score", "error", err)
		return nil, fmt.Errorf("cannot fetch global max score: %w", err)
	}
	if count == 0 {
		return &storage.SearchResult{}, nil
	}
	slog.Info("Computed global max score", "max_score", globalMaxScore, "total_matches", count)

	var searchSQL string
	var args []interface{}

	if slop == 0 {
		// Exact phrase - use parameterized query
		if cursor == nil {
			searchSQL = fmt.Sprintf(`
				SELECT
					id, title, subtitle, content, author, description, url, language, created_at, metadata,
					%s as rank
				FROM articles
				WHERE %s
				ORDER BY rank DESC, id DESC
				LIMIT $2
			`, rankExpr, whereClause)
			args = []interface{}{query.Query, size + 1}
		} else {
			searchSQL = fmt.Sprintf(`
				SELECT
					id, title, subtitle, content, author, description, url, language, created_at, metadata,
					%s as rank
				FROM articles
				WHERE %s
				  AND (%s, id) < ($2, $3)
				ORDER BY rank DESC, id DESC
				LIMIT $4
			`, rankExpr, whereClause, rankExpr)
			args = []interface{}{query.Query, cursor.Score, cursor.ID, size + 1}
		}
	} else {
		// Slop > 0 - query is embedded in expression
		if cursor == nil {
			searchSQL = fmt.Sprintf(`
				SELECT
					id, title, subtitle, content, author, description, url, language, created_at, metadata,
					%s as rank
				FROM articles
				WHERE %s
				ORDER BY rank DESC, id DESC
				LIMIT $1
			`, rankExpr, whereClause)
			args = []interface{}{size + 1}
		} else {
			searchSQL = fmt.Sprintf(`
				SELECT
					id, title, subtitle, content, author, description, url, language, created_at, metadata,
					%s as rank
				FROM articles
				WHERE %s
				  AND (%s, id) < ($1, $2)
				ORDER BY rank DESC, id DESC
				LIMIT $3
			`, rankExpr, whereClause, rankExpr)
			args = []interface{}{cursor.Score, cursor.ID, size + 1}
		}
	}

	rows, err := r.db.Query(ctx, searchSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute phrase search query: %w", err)
	}
	defer rows.Close()

	var articles []dto.ArticleSearchResult
	var rawScores []float64

	for rows.Next() {
		var metadataJSON []byte
		var rawScore float64
		var article dto.Article

		if err := rows.Scan(
			&article.ID,
			&article.Title,
			&article.Subtitle,
			&article.Content,
			&article.Author,
			&article.Description,
			&article.URL,
			&article.Language,
			&article.CreatedAt,
			&metadataJSON,
			&rawScore,
		); err != nil {
			return nil, fmt.Errorf("failed to scan article: %w", err)
		}

		if err := json.Unmarshal(metadataJSON, &article.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}

		searchResult := dto.ArticleSearchResult{
			Article:         article,
			Score:           utils.RoundFloat64(rawScore, dquery.ScoreDecimalPlaces),
			ScoreNormalized: utils.RoundFloat64(rawScore/globalMaxScore, dquery.ScoreDecimalPlaces),
		}

		articles = append(articles, searchResult)
		rawScores = append(rawScores, rawScore)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	slog.Info("PG phrase search results fetched",
		"total_page_matches", len(articles),
		"global_max_score", globalMaxScore)

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
		MaxScore:     utils.RoundFloat64(globalMaxScore, dquery.ScoreDecimalPlaces),
		PageMaxScore: utils.RoundFloat64(rawScores[0], dquery.ScoreDecimalPlaces),
		TotalMatches: count,
	}, nil
}

func (r *Searcher) SearchBoolean(ctx context.Context, query *dquery.Boolean, baseOpts *dquery.BaseOptions) (*storage.SearchResult, error) {
	cursor, size := baseOpts.Cursor, baseOpts.Size
	lang := query.GetLanguage()

	slog.Info("Executing pool boolean search",
		"expression", query.Expression,
		"language", lang,
		"has_cursor", cursor != nil,
		"size", size)

	boolParser := NewBooleanParser()
	tsqueryStr, err := boolParser.Parse(query.Expression)
	if err != nil {
		return nil, fmt.Errorf("failed to parse boolean expression: %w", err)
	}

	slog.Debug("Parsed boolean expression", "input", query.Expression, "tsquery", tsqueryStr)

	queryExpr := fmt.Sprintf("to_tsquery('%s'::regconfig, $1)", lang)
	whereClause := fmt.Sprintf("search_vector @@ %s", queryExpr)
	rankExpr := fmt.Sprintf("ts_rank(search_vector, %s)", queryExpr)

	var globalMaxScore float64
	var count int64
	maxSQL := fmt.Sprintf(`
		SELECT COALESCE(MAX(%s), 0.0) as max_score, COUNT(*)
		FROM articles
		WHERE %s
	`, rankExpr, whereClause)

	if err := r.db.QueryRow(ctx, maxSQL, tsqueryStr).Scan(&globalMaxScore, &count); err != nil {
		slog.Error("Failed to fetch global max score", "error", err)
		return nil, fmt.Errorf("cannot fetch global max score: %w", err)
	}
	if count == 0 {
		return &storage.SearchResult{}, nil
	}
	slog.Info("Computed global max score", "max_score", globalMaxScore, "total_matches", count)

	var searchSQL string
	var args []interface{}

	if cursor == nil {
		searchSQL = fmt.Sprintf(`
			SELECT
				id, title, subtitle, content, author, description, url, language, created_at, metadata,
				%s as rank
			FROM articles
			WHERE %s
			ORDER BY rank DESC, id DESC
			LIMIT $2
		`, rankExpr, whereClause)
		args = []interface{}{tsqueryStr, size + 1}
	} else {
		searchSQL = fmt.Sprintf(`
			SELECT
				id, title, subtitle, content, author, description, url, language, created_at, metadata,
				%s as rank
			FROM articles
			WHERE %s
			  AND (%s, id) < ($2, $3)
			ORDER BY rank DESC, id DESC
			LIMIT $4
		`, rankExpr, whereClause, rankExpr)
		args = []interface{}{tsqueryStr, cursor.Score, cursor.ID, size + 1}
	}

	rows, err := r.db.Query(ctx, searchSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute boolean search query: %w", err)
	}
	defer rows.Close()

	var articles []dto.ArticleSearchResult
	var rawScores []float64

	for rows.Next() {
		var metadataJSON []byte
		var rawScore float64
		var article dto.Article

		if err := rows.Scan(
			&article.ID,
			&article.Title,
			&article.Subtitle,
			&article.Content,
			&article.Author,
			&article.Description,
			&article.URL,
			&article.Language,
			&article.CreatedAt,
			&metadataJSON,
			&rawScore,
		); err != nil {
			return nil, fmt.Errorf("failed to scan article: %w", err)
		}

		if err := json.Unmarshal(metadataJSON, &article.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}

		searchResult := dto.ArticleSearchResult{
			Article:         article,
			Score:           utils.RoundFloat64(rawScore, dquery.ScoreDecimalPlaces),
			ScoreNormalized: utils.RoundFloat64(rawScore/globalMaxScore, dquery.ScoreDecimalPlaces),
		}

		articles = append(articles, searchResult)
		rawScores = append(rawScores, rawScore)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	slog.Info("PG boolean search results fetched",
		"total_page_matches", len(articles),
		"global_max_score", globalMaxScore)

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
		MaxScore:     utils.RoundFloat64(globalMaxScore, dquery.ScoreDecimalPlaces),
		PageMaxScore: utils.RoundFloat64(rawScores[0], dquery.ScoreDecimalPlaces),
		TotalMatches: count,
	}, nil
}

// Compile-time interface assertions
var _ storage.FtsSearcher = (*Searcher)(nil)
