package query

import (
	"fmt"
	"strings"
)

// Fuzziness is the Levenshtein edit distance a term may be from a match.
type Fuzziness string

const (
	NoFuzziness   Fuzziness = "NONE"
	FuzzinessAuto Fuzziness = "AUTO"
	Fuzziness0    Fuzziness = "0"
	Fuzziness1    Fuzziness = "1"
	Fuzziness2    Fuzziness = "2"
)

// ParseFuzziness is case-insensitive and returns NoFuzziness for an empty value.
func ParseFuzziness(raw string) (Fuzziness, error) {
	if raw == "" {
		return NoFuzziness, nil
	}
	switch fuzziness := Fuzziness(strings.ToUpper(raw)); fuzziness {
	case NoFuzziness, FuzzinessAuto, Fuzziness0, Fuzziness1, Fuzziness2:
		return fuzziness, nil
	default:
		return "", fmt.Errorf("unsupported fuzziness: %q (must be NONE, AUTO, 0, 1 or 2)", raw)
	}
}
