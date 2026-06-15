package dto

import (
	"encoding/json"
	"fmt"

	"github.com/DjordjeVuckovic/tusker/internal/apperr"
	"github.com/DjordjeVuckovic/tusker/internal/types/operator"
	"github.com/DjordjeVuckovic/tusker/internal/types/query"
)

// SearchRequest represents the base search request with unified structure
// All search queries follow the pattern: {"size": N, "cursor": "...", "query": {"query_type": {...}}}
//
// Example Match:
//
//	{
//	  "size": 10,
//	  "cursor": "base64...",
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
// Example MultiMatch:
//
//	{
//	  "size": 10,
//	  "query": {
//	    "multi_match": {
//	      "query": "climate change",
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
type SearchRequest struct {
	Size   int          `json:"size,omitempty" validate:"omitempty,min=1"`
	Cursor string       `json:"cursor,omitempty"`
	Query  QueryWrapper `json:"query"`
}

// SearchResponse represents the API response for full-text search
// This is a concrete type for Swagger documentation (swag doesn't support generics yet)
type SearchResponse struct {
	NextCursor   *string               `json:"next_cursor,omitempty"`
	HasMore      bool                  `json:"has_more"`
	MaxScore     float64               `json:"max_score,omitempty"`
	PageMaxScore float64               `json:"page_max_score,omitempty"`
	TotalMatches int64                 `json:"total_matches,omitempty"`
	Hits         []ArticleSearchResult `json:"hits"`
}

// QueryWrapper wraps the actual query type
// Only one query field should be non-nil
type QueryWrapper struct {
	Match      *MatchParams      `json:"match,omitempty"`
	MultiMatch *MultiMatchParams `json:"multi_match,omitempty"`
	Phrase     *PhraseParams     `json:"phrase,omitempty"`
	Boolean    *BooleanParams    `json:"boolean,omitempty"`
	Hybrid     *HybridParams     `json:"hybrid,omitempty"`
}

// MatchParams represents match query parameters (maps directly to types)
type MatchParams struct {
	Query     string `json:"query" validate:"required,min=1"`
	Field     string `json:"field" validate:"required"`
	Operator  string `json:"operator,omitempty"`
	Fuzziness string `json:"fuzziness,omitempty"`
	Language  string `json:"language,omitempty"`
}

// MultiMatchParams represents multi_match query parameters (maps directly to types)
type MultiMatchParams struct {
	Query        string             `json:"query" validate:"required,min=1"`
	Fields       []string           `json:"fields" validate:"required,min=1"`
	FieldWeights map[string]float64 `json:"field_weights,omitempty"`
	Operator     string             `json:"operator,omitempty"`
	Language     string             `json:"language,omitempty"`
}

// PhraseParams represents phrase query parameters (maps directly to types)
// Example:
//
//	{
//	  "query": "climate change",
//	  "fields": ["title", "description"],
//	  "slop": 2,
//	  "language": "english"
//	}
type PhraseParams struct {
	Query    string   `json:"query" validate:"required,min=1"`
	Fields   []string `json:"fields" validate:"required,min=1"`
	Slop     int      `json:"slop,omitempty" validate:"min=0,max=3"`
	Language string   `json:"language,omitempty"`
}

// HybridParams represents hybrid (lexical FTS + vector) query parameters.
type HybridParams struct {
	Query    string `json:"query" validate:"required,min=1"`
	Language string `json:"language,omitempty"`
	K        int    `json:"k,omitempty" validate:"omitempty,min=1"`
}

func (p *HybridParams) ToDomain() (*query.Hybrid, error) {
	if p.Query == "" {
		return nil, apperr.NewValidation("query is required")
	}

	var opts []query.HybridOption

	if p.Language != "" {
		lang := query.Language(p.Language)
		if !query.SupportedLanguages[lang] {
			return nil, apperr.NewValidation(fmt.Sprintf("unsupported language: %s", p.Language))
		}
		opts = append(opts, query.WithHybridLanguage(lang))
	}

	if p.K > 0 {
		opts = append(opts, query.WithHybridK(p.K))
	}

	return query.NewHybrid(p.Query, opts...), nil
}

func (p *MatchParams) ToDomain() (*query.Match, error) {
	if p.Query == "" {
		return nil, apperr.NewValidation("query is required")
	}
	if p.Field == "" {
		return nil, apperr.NewValidation("field is required")
	}

	var opts []query.MatchQueryOption

	op, err := operator.Parse(p.Operator)
	if err != nil {
		return nil, apperr.NewValidationWrap("invalid operator", err)
	}
	opts = append(opts, query.WithMatchOperator(op))

	if p.Fuzziness != "" {
		opts = append(opts, query.WithMatchFuzziness(p.Fuzziness))
	}

	if p.Language != "" {
		lang := query.Language(p.Language)
		if !query.SupportedLanguages[lang] {
			return nil, apperr.NewValidation(fmt.Sprintf("unsupported language: %s", p.Language))
		}
		opts = append(opts, query.WithMatchLanguage(lang))
	}

	return query.NewMatch(p.Field, p.Query, opts...), nil
}

func (p *MultiMatchParams) ToDomain() (*query.MultiMatch, error) {
	var opts []query.MultiMatchQueryOption

	op, err := operator.Parse(p.Operator)
	if err != nil {
		return nil, apperr.NewValidationWrap("invalid operator", err)
	}
	opts = append(opts, query.WithMultiMatchOperator(op))

	if p.Language != "" {
		lang := query.Language(p.Language)
		if !query.SupportedLanguages[lang] {
			return nil, apperr.NewValidation(fmt.Sprintf("unsupported language: %s", p.Language))
		}
		opts = append(opts, query.WithMultiMatchLanguage(lang))
	}

	newQuery, err := query.NewMultiMatchQuery(p.Query, p.Fields, opts...)
	if err != nil {
		return nil, apperr.NewValidationWrap("invalid input", err)
	}

	return newQuery, nil
}

type BooleanParams struct {
	Expression string `json:"expression" validate:"required,min=1"`
	Language   string `json:"language,omitempty"`
}

func (p *BooleanParams) ToDomain() (*query.Boolean, error) {
	if p.Expression == "" {
		return nil, apperr.NewValidation("expression is required")
	}

	b := &query.Boolean{
		Expression: p.Expression,
	}

	if p.Language != "" {
		lang := query.Language(p.Language)
		if !query.SupportedLanguages[lang] {
			return nil, apperr.NewValidation(fmt.Sprintf("unsupported language: %s", p.Language))
		}
		b.Language = lang
	}

	return b, nil
}

func (p *PhraseParams) ToDomain() (*query.Phrase, error) {
	if p.Query == "" {
		return nil, apperr.NewValidation("query is required")
	}
	if len(p.Fields) == 0 {
		return nil, apperr.NewValidation("fields are required (at least one field)")
	}

	var opts []query.PhraseOption

	if p.Slop > 0 {
		opts = append(opts, query.WithPhraseSlop(p.Slop))
	}

	if p.Language != "" {
		lang := query.Language(p.Language)
		if !query.SupportedLanguages[lang] {
			return nil, apperr.NewValidation(fmt.Sprintf("unsupported language: %s", p.Language))
		}
		opts = append(opts, query.WithPhraseLanguage(lang))
	}

	newQuery, err := query.NewPhrase(p.Query, p.Fields, opts...)
	if err != nil {
		return nil, apperr.NewValidationWrap("invalid phrase query", err)
	}

	return newQuery, nil
}

// GetQueryType returns the type of query in the wrapper
func (q *QueryWrapper) GetQueryType() query.Kind {
	if q.Match != nil {
		return query.MatchType
	}
	if q.MultiMatch != nil {
		return query.MultiMatchType
	}
	if q.Phrase != nil {
		return query.PhraseType
	}
	if q.Boolean != nil {
		return query.BooleanType
	}
	if q.Hybrid != nil {
		return query.HybridType
	}
	return ""
}

type SemanticSearchRequest struct {
	Query  string `json:"query"`
	Size   int    `json:"size,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

type SemanticSearchResponse struct {
	NextCursor *string   `json:"next_cursor,omitempty"`
	HasMore    bool      `json:"has_more"`
	Hits       []Article `json:"hits"`
}

func (p *SemanticSearchRequest) ToDomain() (*query.Semantic, error) {
	if p.Query == "" {
		return nil, apperr.NewValidation("query is required")
	}

	return query.NewSemantic(p.Query), nil
}

// UnmarshalJSON implements custom JSON unmarshaling with validation
func (q *QueryWrapper) UnmarshalJSON(data []byte) error {
	type Alias QueryWrapper
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(q),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Validate that exactly one query type is provided
	count := 0
	if q.Match != nil {
		count++
	}
	if q.MultiMatch != nil {
		count++
	}
	if q.Phrase != nil {
		count++
	}
	if q.Boolean != nil {
		count++
	}
	if q.Hybrid != nil {
		count++
	}

	if count == 0 {
		return apperr.NewValidation("query must specify one of: match, multi_match, phrase, boolean, hybrid")
	}
	if count > 1 {
		return apperr.NewValidation("query must specify only one query type")
	}

	return nil
}
