package native

import (
	"fmt"
	"math"
	"slices"
	"strconv"
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

// isolatedFields are the searchable fields that alone fill their weight band.
func isolatedFields() []query.Field {
	fieldsPerLabel := map[string]int{}
	for _, label := range fieldToLabel {
		fieldsPerLabel[label]++
	}
	var isolated []query.Field
	for _, field := range query.SearchableFields() {
		if fieldsPerLabel[fieldToLabel[field]] == 1 {
			isolated = append(isolated, field)
		}
	}
	return isolated
}

// Label to ts_rank weight array position mapping
// PostgreSQL weights array format: {D, C, B, A} (reverse order!)
var labelToPosition = map[string]int{
	"A": 3, // Title - position 3 in {D, C, B, A}
	"B": 2, // Description - position 2
	"C": 1, // Content - position 1
	"D": 0, // Subtitle/Author - position 0
}

// buildWeightLabels converts field names to PostgreSQL weight label string
// Examples:
//
//	["title", "description"] → "AB"
//	["title", "content"]     → "AC"
//	["title"]                → "A"
//	[]                       → "" (empty means search all fields)
func buildWeightLabels(fields []query.Field) string {
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

// buildWeightsArray creates the ts_rank weights array from field boosts, in
// PostgreSQL's {D, C, B, A} order. ts_rank rejects weights above 1, so boosts
// are scaled by the largest one when it exceeds 1. Weights are float4, written
// at that precision so a small ratio is not rounded to 0.
// Example: [{title 3.0} {description 1.5}] → "{0, 0, 0.5, 1}"
func buildWeightsArray(fieldBoosts []query.FieldWeight) string {
	weights := [4]float64{0.0, 0.0, 0.0, 0.0}

	for _, fb := range fieldBoosts {
		if label, ok := fieldToLabel[fb.Field]; ok {
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

	formatted := make([]string, 0, len(weights))
	for _, w := range weights {
		formatted = append(formatted, strconv.FormatFloat(w, 'g', -1, 32))
	}
	return "{" + strings.Join(formatted, ", ") + "}"
}

// buildTsQuery turns free text into a tsquery: every term under AND, any term
// under OR. plainto_tsquery joins terms with " & ", so OR swaps each for " | ",
// leaving a lexeme that holds & itself (a URL) intact; the lexemes are already
// normalised and the cast does not stem them again.
func buildTsQuery(op operator.Operator, lang query.Language, paramNum int) string {
	plain := fmt.Sprintf("plainto_tsquery('%s'::regconfig, $%d)", lang, paramNum)
	if op.IsOr() {
		return fmt.Sprintf("replace(%s::text, ' & ', ' | ')::tsquery", plain)
	}
	return plain
}

// buildRankExpression constructs a ts_rank expression with custom field weights
// The pre-computed search_vector has weights: title=A, description=B, content=C, subtitle/author=D
// PostgreSQL's default weight values are: {0.1, 0.2, 0.4, 1.0} for {D, C, B, A}
// Weight array format: {D-weight, C-weight, B-weight, A-weight} (REVERSE ORDER!)
// Returns: "ts_rank('{0.0, 1.0, 1.5, 3.0}', search_vector, query)" or "ts_rank(search_vector, query)"
func buildRankExpression(fieldBoosts []query.FieldWeight, lang query.Language, op operator.Operator, paramNum int) string {
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

// DefaultOperator is what Postgres applies when a request names no operator:
// plainto_tsquery requires every term.
const DefaultOperator = operator.And

// buildTsWhereClause matches free text against the requested fields only.
func buildTsWhereClause(fieldBoosts []query.FieldWeight, lang query.Language, op operator.Operator, paramNum int) string {
	return buildFieldMatch(fieldBoosts, buildTsQuery(op, lang, paramNum), true)
}

// buildFieldMatch matches queryExpr on the fields' weight bands. No fields, or
// fields covering all four bands, match the whole vector. With prefilter the
// whole-vector match goes first so the GIN index narrows the rows; that is only
// sound when a band match implies a whole-vector match, which a negated term
// breaks.
func buildFieldMatch(fieldBoosts []query.FieldWeight, queryExpr string, prefilter bool) string {
	match := fmt.Sprintf("search_vector @@ %s", queryExpr)

	fields := make([]query.Field, 0, len(fieldBoosts))
	for _, fb := range fieldBoosts {
		fields = append(fields, fb.Field)
	}
	labels := buildWeightLabels(fields)
	if labels == "" || labels == "ABCD" {
		return match
	}

	bands := strings.Split(strings.ToLower(labels), "")
	bandMatch := fmt.Sprintf("ts_filter(search_vector, '{%s}') @@ %s", strings.Join(bands, ","), queryExpr)
	if !prefilter {
		return bandMatch
	}
	return fmt.Sprintf("%s AND %s", match, bandMatch)
}

// buildPhraseSlopQuery joins tsquery lexemes, already quoted, into the phrase
// with every gap of up to slop words between neighbours. <N> means N-1 words
// apart, so slop 2 for ['climat' 'chang'] gives
// 'climat' <-> 'chang' | 'climat' <2> 'chang' | 'climat' <3> 'chang'.
func buildPhraseSlopQuery(lexemes []string, slop int) string {
	alternatives := make([]string, 0, slop+1)
	for distance := 0; distance <= slop; distance++ {
		link := "<->"
		if distance > 0 {
			link = fmt.Sprintf("<%d>", distance+1)
		}
		pairs := make([]string, 0, len(lexemes)-1)
		for i := 0; i+1 < len(lexemes); i++ {
			pairs = append(pairs, fmt.Sprintf("%s %s %s", lexemes[i], link, lexemes[i+1]))
		}
		alternatives = append(alternatives, strings.Join(pairs, " & "))
	}
	return strings.Join(alternatives, " | ")
}

// quotedLexemes returns the lexemes of a tsquery's text form in order, each
// still in its quotes, so they can be put back into a tsquery unchanged. A
// quote inside a lexeme is doubled.
func quotedLexemes(tsquery string) []string {
	var lexemes []string
	for i := 0; i < len(tsquery); i++ {
		if tsquery[i] != '\'' {
			continue
		}
		end := i + 1
		for end < len(tsquery) {
			if tsquery[end] == '\'' {
				if end+1 < len(tsquery) && tsquery[end+1] == '\'' {
					end += 2
					continue
				}
				break
			}
			end++
		}
		lexemes = append(lexemes, tsquery[i:end+1])
		i = end
	}
	return lexemes
}
