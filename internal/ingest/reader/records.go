package reader

import (
	"fmt"
	"iter"
)

// RecordReader yields raw records one at a time in source order. A
// *MalformedRecordError is yielded for a record that cannot be decoded and
// reading continues; any other error ends the sequence.
type RecordReader interface {
	Records() iter.Seq2[map[string]string, error]
}

// MalformedRecordError reports one source record that could not be decoded.
type MalformedRecordError struct {
	Line int64
	Err  error
}

func (e *MalformedRecordError) Error() string {
	return fmt.Sprintf("malformed record at line %d: %v", e.Line, e.Err)
}

func (e *MalformedRecordError) Unwrap() error { return e.Err }
