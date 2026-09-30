package query

import (
	"fmt"

	"github.com/DjordjeVuckovic/tusker/internal/types/operator"
)

// Field is an article field a query may search.
type Field string

const (
	FieldTitle       Field = "title"
	FieldSubtitle    Field = "subtitle"
	FieldDescription Field = "description"
	FieldContent     Field = "content"
	FieldAuthor      Field = "author"
)

// SearchableFields is the allow-list of fields every engine indexes for full-text search.
var SearchableFields = []Field{FieldTitle, FieldSubtitle, FieldDescription, FieldContent, FieldAuthor}

func ParseField(s string) (Field, error) {
	for _, field := range SearchableFields {
		if Field(s) == field {
			return field, nil
		}
	}
	return "", fmt.Errorf("unsupported field: %q (must be one of %v)", s, SearchableFields)
}

type FieldWeight struct {
	Field  Field
	Weight float64
}

// SearchContract is the question a query string asks every engine: which fields
// to match, how to weight them, how to combine terms and how to analyse them.
// Engines build their query from it, so one request means one recall set.
type SearchContract struct {
	Fields   []FieldWeight
	Operator operator.Operator
	Language Language
}

// DefaultSearchContract mirrors what the benchmark templates measure: the
// title^3, description^2, content boosts of the Elasticsearch and ParadeDB
// templates, and the every-term-required semantics of plainto_tsquery.
func DefaultSearchContract() SearchContract {
	return SearchContract{
		Fields: []FieldWeight{
			{Field: FieldTitle, Weight: 3.0},
			{Field: FieldDescription, Weight: 2.0},
			{Field: FieldContent, Weight: 1.0},
		},
		Operator: operator.And,
		Language: DefaultLanguage,
	}
}
