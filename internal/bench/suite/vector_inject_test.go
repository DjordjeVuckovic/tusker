package suite

import (
	"testing"
)

func TestFormatVector(t *testing.T) {
	got := FormatVector([]float32{0.1, 0.2, -0.3})
	if got != "[0.1,0.2,-0.3]" {
		t.Errorf("FormatVector = %q, want %q", got, "[0.1,0.2,-0.3]")
	}
	if got := FormatVector(nil); got != "[]" {
		t.Errorf("FormatVector(nil) = %q, want []", got)
	}
}
