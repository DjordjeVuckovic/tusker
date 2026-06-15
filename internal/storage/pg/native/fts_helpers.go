package native

import (
	"fmt"
	"log/slog"
	"math"
	"strings"

	"github.com/DjordjeVuckovic/tusker/internal/types/operator"
	"github.com/DjordjeVuckovic/tusker/internal/types/query"
)

// Field to PostgreSQL weight label mapping
// Weight labels determine which document sections are searched
var fieldToLabel = map[string]string{
	"title":       "A",
	"description": "B",
	"content":     "C",
	"subtitle":    "D",
	"author":      "D",
}

// Label to ts_rank weight array position mapping
// PostgreSQL weights array format: {D, C, B, A} (reverse order!)
var labelToPosition = map[string]int{
	"A": 3, // Title - position 3 in {D, C, B, A}
	"B": 2, // Description - position 2
	"C": 1, // Content - position 1
	"D": 0, // Subtitle/Author - position 0
}

// FieldWeight represents a field with its boost value for ES-style notation
type FieldWeight struct {
	Field  string
	Weight float64
}

// buildWeightLabels converts field names to PostgreSQL weight label string
// Examples:
//
//	["title", "description"] → "AB"
//	["title", "content"]     → "AC"
//	["title"]                → "A"
//	[]                       → "" (empty means search all fields)
func buildWeightLabels(fields []string) string {
	if len(fields) == 0 {
		return "" // Empty = search all fields
	}

	labels := make(map[string]bool)
	for _, field := range fields {
		if label, ok := fieldToLabel[field]; ok {
			labels[label] = true
		}
	}

	// Build sorted string (ABCD order for consistency)
	result := ""
	for _, label := range []string{"A", "B", "C", "D"} {
		if labels[label] {
			result += label
		}
	}

	return result
}

// buildWeightsArray creates ts_rank weights array from field boosts
// PostgreSQL array format: {D-weight, C-weight, B-weight, A-weight} (reverse order!)
// Examples:
//
//	[]FieldWeight{{"title", 3.0}, {"description", 1.5}}
//	→ "{0.00, 0.00, 1.50, 3.00}"  (D=0.0, C=0.0, B=1.5, A=3.0)
func buildWeightsArray(fieldBoosts []FieldWeight) string {
	// Initialize with zeros - only specified fields will have non-zero weights
	weights := [4]float64{0.0, 0.0, 0.0, 0.0} // {D, C, B, A}

	for _, fb := range fieldBoosts {
		if label, ok := fieldToLabel[fb.Field]; ok {
			position := labelToPosition[label]

			// For D (subtitle/author), take max boost if multiple fields map to D
			if position == 0 {
				weights[position] = math.Max(weights[position], fb.Weight)
			} else {
				weights[position] = fb.Weight
			}
		}
	}

	result := fmt.Sprintf("{%.2f, %.2f, %.2f, %.2f}",
		weights[0], weights[1], weights[2], weights[3])

	// Log for debugging
	slog.Debug("Built weights array",
		"weights_dcba", result,
		"D", weights[0],
		"C", weights[1],
		"B", weights[2],
		"A", weights[3],
		"field_boosts", fieldBoosts)

	return result
}

// buildTsQuery constructs a PostgreSQL tsquery expression based on operator
// paramNum: The parameter number to use ($1, $2, etc.)
// Returns: "plainto_tsquery('english'::regconfig, $1)" or "websearch_to_tsquery(...)"
func buildTsQuery(op operator.Operator, lang query.Language, paramNum int) string {

	if op.IsOr() {
		// websearch_to_tsquery supports OR operator via "term1 OR term2" syntax
		return fmt.Sprintf("websearch_to_tsquery('%s'::regconfig, $%d)", lang, paramNum)
	}

	// plainto_tsquery uses AND by default for simple searches
	// "climate change" -> "climat & chang"
	return fmt.Sprintf("plainto_tsquery('%s'::regconfig, $%d)", lang, paramNum)
}

// buildRankExpression constructs a ts_rank expression with custom field weights
// The pre-computed search_vector has weights: title=A, description=B, content=C, subtitle/author=D
// PostgreSQL's default weight values are: {0.1, 0.2, 0.4, 1.0} for {D, C, B, A}
// Weight array format: {D-weight, C-weight, B-weight, A-weight} (REVERSE ORDER!)
// Returns: "ts_rank('{0.0, 1.0, 1.5, 3.0}', search_vector, query)" or "ts_rank(search_vector, query)"
func buildRankExpression(fieldBoosts []FieldWeight, lang query.Language, op operator.Operator, paramNum int) string {
	vectorExpr := "search_vector"
	queryExpr := buildTsQuery(op, lang, paramNum)

	// If custom boosts specified, use them
	if len(fieldBoosts) > 0 {
		weightsArray := buildWeightsArray(fieldBoosts)
		return fmt.Sprintf("ts_rank('%s', %s, %s)", weightsArray, vectorExpr, queryExpr)
	}

	// Use default PostgreSQL weights
	return fmt.Sprintf("ts_rank(%s, %s)", vectorExpr, queryExpr)
}

// buildTsWhereClause constructs the WHERE clause for full-text search with weight label filtering
// Weight labels filter which fields are searched: A=title, B=description, C=content, D=subtitle/author
// Examples:
//
//	fieldBoosts=[{title,3.0}]                       → search_vector @@ (query::text || ':A')::tsquery
//	fieldBoosts=[{title,3.0},{description,1.5}]     → search_vector @@ (query::text || ':AB')::tsquery
//	fieldBoosts=[]                                   → search_vector @@ query (all fields)
func buildTsWhereClause(fieldBoosts []FieldWeight, lang query.Language, op operator.Operator, paramNum int) string {
	vectorExpr := "search_vector"
	queryExpr := buildTsQuery(op, lang, paramNum)

	// Extract field names from FieldWeight
	fields := make([]string, 0, len(fieldBoosts))
	for _, fb := range fieldBoosts {
		fields = append(fields, fb.Field)
	}

	// Build weight labels from field names
	labels := buildWeightLabels(fields)

	var result string
	// If specific fields requested, use weight label filtering
	if labels != "" {
		result = fmt.Sprintf("%s @@ (%s::text || ':%s')::tsquery", vectorExpr, queryExpr, labels)
		slog.Debug("Built WHERE clause with label filtering",
			"labels", labels,
			"fields", fields,
			"where_clause", result)
	} else {
		// No field filtering - search all fields
		result = fmt.Sprintf("%s @@ %s", vectorExpr, queryExpr)
		slog.Debug("Built WHERE clause without filtering",
			"where_clause", result)
	}

	return result
}

// buildPhraseSlopQuery constructs a phrase query with slop support
// This generates an OR query with multiple distance operators
//
// Example for "climate change" with slop=2:
//
//	Input: ["climat", "chang"], slop=2
//	Output: "climat <-> chang | climat <2> chang | climat <3> chang"
//
// PostgreSQL distance operator <N> means exactly N-1 lexemes apart:
//   - <-> means adjacent (0 words between)
//   - <2> means 1 word between
//   - <3> means 2 words between
func buildPhraseSlopQuery(tokens []string, slop int) string {
	if len(tokens) < 2 {
		// Single token - just return it
		if len(tokens) == 1 {
			return tokens[0]
		}
		return ""
	}

	var orParts []string

	// Generate OR expressions for each distance from 0 to slop
	// distance 0: term1 <-> term2 (adjacent)
	// distance 1: term1 <2> term2 (one word apart)
	// distance 2: term1 <3> term2 (two words apart)
	for distance := 0; distance <= slop; distance++ {
		var parts []string
		for i := 0; i < len(tokens)-1; i++ {
			if distance == 0 {
				parts = append(parts, fmt.Sprintf("%s <-> %s", tokens[i], tokens[i+1]))
			} else {
				// distance+1 because <2> means 1 word between, <3> means 2 words between
				parts = append(parts, fmt.Sprintf("%s <%d> %s", tokens[i], distance+1, tokens[i+1]))
			}
		}

		// Join consecutive token pairs with &
		if len(parts) > 0 {
			orParts = append(orParts, strings.Join(parts, " & "))
		}
	}

	// Join all distance variants with OR
	return strings.Join(orParts, " | ")
}

// extractLexemesFromTsquery extracts lexemes from a tsquery string
// Example: "'climat' & 'chang'" -> ["climat", "chang"]
// Example: "'renew' & 'energi'" -> ["renew", "energi"]
func extractLexemesFromTsquery(tsqueryStr string) []string {
	var lexemes []string

	// Replace operators with spaces to make splitting easier
	cleaned := strings.ReplaceAll(tsqueryStr, "&", " ")
	cleaned = strings.ReplaceAll(cleaned, "|", " ")
	cleaned = strings.ReplaceAll(cleaned, "!", " ")

	// Split by whitespace
	parts := strings.Fields(cleaned)

	for _, part := range parts {
		// Remove single quotes around lexemes
		trimmed := strings.Trim(part, "'")
		if trimmed != "" && trimmed != "(" && trimmed != ")" {
			lexemes = append(lexemes, trimmed)
		}
	}

	return lexemes
}
