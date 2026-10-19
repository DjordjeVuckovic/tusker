package reader

import (
	"fmt"
	"iter"
)

// RecordReader yields records in source order; it keeps going past a
// *MalformedRecordError, and any other error ends it.
type RecordReader interface {
	Records() iter.Seq2[map[string]string, error]
}

type MalformedRecordError struct {
	Line int64
	Err  error
}

func (e *MalformedRecordError) Error() string {
	return fmt.Sprintf("malformed record at line %d: %v", e.Line, e.Err)
}

func (e *MalformedRecordError) Unwrap() error { return e.Err }
