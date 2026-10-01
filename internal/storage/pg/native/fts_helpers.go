package native

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/DjordjeVuckovic/tusker/internal/types/operator"
	"github.com/DjordjeVuckovic/tusker/internal/types/query"
)

// Field to PostgreSQL weight label mapping
// Weight labels determine which document sections are searched
var fieldToLabel = map[query.Field]string{
	query.FieldTitle:       "A",
	query.FieldDescription: "B",
	query.FieldContent:     "C",
	query.FieldSubtitle:    "D",
	query.FieldAuthor:      "D",
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
		if label, ok := fieldToLabel[query.Field(field)]; ok {
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

// buildWeightsArray creates the ts_rank weights array from field boosts, in
// PostgreSQL's {D, C, B, A} order. ts_rank rejects weights above 1, so boosts
// are scaled by the largest one when it exceeds 1; ranking only depends on
// their ratio.
// Example: [{title 3.0} {description 1.5}] → "{0.0000, 0.0000, 0.5000, 1.0000}"
func buildWeightsArray(fieldBoosts []FieldWeight) string {
	weights := [4]float64{0.0, 0.0, 0.0, 0.0}

	for _, fb := range fieldBoosts {
		if label, ok := fieldToLabel[query.Field(fb.Field)]; ok {
			position := labelToPosition[label]
			weights[position] = math.Max(weights[position], fb.Weight)
		}
	}

	largest := slices.Max(weights[:])
	if largest > 1 {
		for i := range weights {
			weights[i] /= largest
		}
	}

	return fmt.Sprintf("{%.4f, %.4f, %.4f, %.4f}", weights[0], weights[1], weights[2], weights[3])
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

// buildTsWhereClause matches the query against the requested fields only.
// No fields, or fields covering all four bands, match the whole vector.
// Otherwise the query runs on the fields' weight bands. Under AND that implies
// a match on the whole vector, which goes first so the GIN index narrows the
// rows. Under OR the query may negate a term, so a row whose fields qualify can
// fail the whole vector; there the bands are matched alone.
func buildTsWhereClause(fieldBoosts []FieldWeight, lang query.Language, op operator.Operator, paramNum int) string {
	queryExpr := buildTsQuery(op, lang, paramNum)
	match := fmt.Sprintf("search_vector @@ %s", queryExpr)

	fields := make([]string, 0, len(fieldBoosts))
	for _, fb := range fieldBoosts {
		fields = append(fields, fb.Field)
	}
	labels := buildWeightLabels(fields)
	if labels == "" || labels == "ABCD" {
		return match
	}

	bands := strings.Split(strings.ToLower(labels), "")
	bandMatch := fmt.Sprintf("ts_filter(search_vector, '{%s}') @@ %s", strings.Join(bands, ","), queryExpr)
	if op.IsOr() {
		return bandMatch
	}
	return fmt.Sprintf("%s AND %s", match, bandMatch)
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
