package dto

import (
	"github.com/DjordjeVuckovic/tusker/internal/apperr"
	"github.com/DjordjeVuckovic/tusker/internal/types/operator"
	"github.com/DjordjeVuckovic/tusker/internal/types/query"
)

// parseLanguage returns the default language for an empty value. An unknown
// one is rejected rather than handed to ::regconfig, where it would stem with
// the wrong dictionary.
func parseLanguage(raw string) (query.Language, error) {
	lang, err := query.Language(raw).Parse()
	if err != nil {
		return "", apperr.NewValidationWrap("invalid language", err)
	}
	return lang, nil
}

// parseOperator returns nil for an empty value, so the engine applies its own
// default operator.
func parseOperator(raw string) (*operator.Operator, error) {
	if raw == "" {
		return nil, nil
	}
	op, err := operator.Parse(raw)
	if err != nil {
		return nil, apperr.NewValidationWrap("invalid operator", err)
	}
	return &op, nil
}

// parseField rejects a field outside query.SearchableFields. Postgres would
// otherwise search every field and Elasticsearch none.
func parseField(raw string) (query.Field, error) {
	field, err := query.ParseField(raw)
	if err != nil {
		return "", apperr.NewValidationWrap("invalid field", err)
	}
	return field, nil
}

func parseFields(raw []string) ([]query.Field, error) {
	fields := make([]query.Field, 0, len(raw))
	for _, name := range raw {
		field, err := parseField(name)
		if err != nil {
			return nil, err
		}
		fields = append(fields, field)
	}
	return fields, nil
}

func parseFuzziness(raw string) (query.Fuzziness, error) {
	fuzziness, err := query.ParseFuzziness(raw)
	if err != nil {
		return "", apperr.NewValidationWrap("invalid fuzziness", err)
	}
	return fuzziness, nil
}
