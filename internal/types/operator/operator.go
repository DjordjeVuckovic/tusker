package operator

import (
	"fmt"
	"strings"
)

// Operator represents how multiple search terms should be combined in a query
// Value object following DDD principles with validation and behavior
//
// Usage:
//
//	query.Operator = operator.And  // All terms must match
//	query.Operator = operator.Or   // Any term can match
type Operator string

const (
	// And requires all search terms to match (higher precision, lower recall)
	And Operator = "AND"

	// Or requires any search term to match (lower precision, higher recall)
	Or Operator = "OR"
)

const Default = And

func Parse(s string) (Operator, error) {
	if s == "" {
		return Default, nil
	}

	op := Operator(strings.ToUpper(s))
	switch op {
	case Or, And:
		return op, nil
	default:
		return "", fmt.Errorf("invalid operator: %s (must be 'or' or 'and')", s)
	}
}

// String returns the string representation of the operator
func (o Operator) String() string {
	return string(o)
}

// IsAnd returns true if the operator is AND
func (o Operator) IsAnd() bool {
	return o == And
}

// IsOr returns true if the operator is OR
func (o Operator) IsOr() bool {
	return o == Or
}

// Validate ensures the operator has a valid value
func (o Operator) Validate() error {
	if o != And && o != Or {
		return fmt.Errorf("invalid operator: %q (must be 'and' or 'or')", o)
	}
	return nil
}

// MarshalText implements encoding.TextMarshaler for JSON serialization
func (o Operator) MarshalText() ([]byte, error) {
	return []byte(o.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler for JSON deserialization
func (o *Operator) UnmarshalText(text []byte) error {
	op, err := Parse(string(text))
	if err != nil {
		return err
	}
	*o = op
	return nil
}
