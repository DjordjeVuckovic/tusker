package dto

import (
	"github.com/DjordjeVuckovic/tusker/internal/apperr"
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
