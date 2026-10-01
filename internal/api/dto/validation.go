package dto

import (
	"fmt"
	"strings"

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

func parseOperator(raw string) (operator.Operator, error) {
	op, err := operator.Parse(raw)
	if err != nil {
		return "", apperr.NewValidationWrap("invalid operator", err)
	}
	return op, nil
}

// validateField rejects a field outside query.SearchableFields. Postgres would
// otherwise search every field and Elasticsearch none.
func validateField(raw string) error {
	if _, err := query.ParseField(raw); err != nil {
		return apperr.NewValidationWrap("invalid field", err)
	}
	return nil
}

func validateFields(raw []string) error {
	for _, field := range raw {
		if err := validateField(field); err != nil {
			return err
		}
	}
	return nil
}

// parseFuzziness returns "" when no fuzziness was asked for.
func parseFuzziness(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	fuzziness := query.Fuzziness(strings.ToUpper(raw))
	if !query.SupportedFuzziness[fuzziness] {
		return "", apperr.NewValidation(fmt.Sprintf("unsupported fuzziness: %q (must be AUTO, 0, 1 or 2)", raw))
	}
	return string(fuzziness), nil
}
