package query

import (
	"fmt"
	"slices"
	"strings"

	"github.com/DjordjeVuckovic/tusker/internal/types/operator"
)

// Capabilities is what a full-text engine can honour within the query types it
// serves.
type Capabilities struct {
	Engine string `json:"engine"`
	// DefaultOperator combines terms when a request names no operator.
	DefaultOperator operator.Operator `json:"default_operator"`
	Fuzziness       bool              `json:"fuzziness"`
	PhraseSlop      bool              `json:"phrase_slop"`
	Boosts          bool              `json:"boosts"`
	// Fields are the fields the engine can match apart from every other field.
	Fields []Field `json:"fields"`
}

// Params are the parameters a request states that an engine may be unable to
// honour. A field weight other than 1 is a boost.
type Params struct {
	Fields    []FieldWeight
	Fuzziness Fuzziness
	Slop      int
}

// Ignored is a stated parameter the engine did not honour.
type Ignored struct {
	Param  string `json:"param"`
	Engine string `json:"engine"`
	Reason string `json:"reason"`
}

// UnsupportedParamError rejects a parameter that would change which articles
// match on an engine that cannot honour it.
type UnsupportedParamError struct {
	Param  string
	Engine string
	Reason string
}

func (e *UnsupportedParamError) Error() string {
	return fmt.Sprintf("%s is not supported by %s: %s", e.Param, e.Engine, e.Reason)
}

// Handling decides what happens to a parameter that changes the match set and
// that the engine cannot honour.
type Handling string

const (
	// HandlingStrict rejects the request.
	HandlingStrict Handling = "strict"
	// HandlingLenient runs the request without the parameter and reports it.
	HandlingLenient Handling = "lenient"
)

// ParseHandling is case-insensitive and returns HandlingStrict for an empty value.
func ParseHandling(raw string) (Handling, error) {
	switch handling := Handling(strings.ToLower(raw)); handling {
	case "":
		return HandlingStrict, nil
	case HandlingStrict, HandlingLenient:
		return handling, nil
	default:
		return "", fmt.Errorf("unsupported handling: %q (must be strict or lenient)", raw)
	}
}

// CheckCapabilities compares the stated params with what the engine honours.
// A param that changes the match set fails the check under HandlingStrict and
// is listed as ignored under HandlingLenient. A boost only changes ranking, so
// an unsupported one is always listed and never fails the check.
func CheckCapabilities(caps Capabilities, params Params, handling Handling) ([]Ignored, error) {
	var ignored []Ignored
	ignore := func(param, reason string) {
		ignored = append(ignored, Ignored{Param: param, Engine: caps.Engine, Reason: reason})
	}

	if params.Fuzziness.IsFuzzy() && !caps.Fuzziness {
		ignore("fuzziness", "terms match exactly")
	}
	if params.Slop > 0 && !caps.PhraseSlop {
		ignore("slop", "phrases match exactly")
	}
	for _, field := range params.Fields {
		if !slices.Contains(caps.Fields, field.Field) {
			ignore("fields", fmt.Sprintf("%s cannot be matched apart from other fields", field.Field))
		}
	}
	if handling != HandlingLenient && len(ignored) > 0 {
		first := ignored[0]
		return nil, &UnsupportedParamError{Param: first.Param, Engine: first.Engine, Reason: first.Reason}
	}

	for _, field := range params.Fields {
		boosted := field.Weight != 1
		if boosted && (!caps.Boosts || !slices.Contains(caps.Fields, field.Field)) {
			ignore("boost", fmt.Sprintf("%s^%g is not applied to ranking", field.Field, field.Weight))
		}
	}
	return ignored, nil
}
