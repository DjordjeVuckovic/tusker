package dto

import (
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/DjordjeVuckovic/tusker/internal/apperr"
	"github.com/DjordjeVuckovic/tusker/internal/types/query"
	"github.com/DjordjeVuckovic/tusker/pkg/pagination"
)

// MaxQueryLength caps query text in characters, not bytes, so a Serbian
// Cyrillic query gets the same room as a Latin one.
const MaxQueryLength = 1000

func checkQueryLength(name, text string) error {
	if utf8.RuneCountInString(text) > MaxQueryLength {
		return apperr.NewValidation(fmt.Sprintf("%s exceeds the maximum of %d characters", name, MaxQueryLength))
	}
	return nil
}

// NewStringQuery converts the text of a simple query-string search.
func NewStringQuery(text string) (*query.String, error) {
	if err := checkQueryLength("q", text); err != nil {
		return nil, err
	}
	return query.NewQueryString(text), nil
}

// PageSize returns the page size to search with: the default when requested
// is zero, a validation error when it is negative or over the maximum.
func PageSize(requested int) (int, error) {
	switch {
	case requested == 0:
		return pagination.PageDefaultSize, nil
	case requested < 0:
		return 0, apperr.NewValidation("size parameter must be positive")
	case requested > pagination.PageMaxSize:
		return 0, apperr.NewValidation(fmt.Sprintf("size parameter exceeds maximum of %d", pagination.PageMaxSize))
	}
	return requested, nil
}

// ParsePageSize is PageSize for a size query parameter, where an empty value
// means unset and zero is invalid.
func ParsePageSize(raw string) (int, error) {
	if raw == "" {
		return pagination.PageDefaultSize, nil
	}
	size, err := strconv.Atoi(raw)
	if err != nil || size < 1 {
		return 0, apperr.NewValidation("invalid size parameter")
	}
	return PageSize(size)
}
