package reader

import "fmt"

// DropReason classifies why a record could not be mapped
type DropReason string

const (
	ReasonMissingColumn DropReason = "missing_source_column"
	ReasonEmptyValue    DropReason = "empty_required_value"
	ReasonConversion    DropReason = "conversion_failed"
)

// MappingDropError reports a record the mapper refused to produce
type MappingDropError struct {
	Reason DropReason
	Source string
	Target string
	Err    error
}

func (e *MappingDropError) Error() string {
	switch e.Reason {
	case ReasonMissingColumn:
		return fmt.Sprintf("source column %q (target %s) is not present in the record", e.Source, e.Target)
	case ReasonEmptyValue:
		return fmt.Sprintf("required field %s is empty (source column %q)", e.Target, e.Source)
	default:
		return fmt.Sprintf("required field %s (source column %q): %v", e.Target, e.Source, e.Err)
	}
}

func (e *MappingDropError) Unwrap() error { return e.Err }
