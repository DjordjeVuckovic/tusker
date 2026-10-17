package utils

import (
	"math"
	"testing"
)

func TestRoundFloat64(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		want  float64
	}{
		{name: "rounds up", value: 0.12347, want: 0.1235},
		{name: "rounds down", value: 3.14159, want: 3.1416},
		{name: "negative rounds half away from zero", value: -1.23456, want: -1.2346},
		{name: "negative rounds toward zero", value: -1.23441, want: -1.2344},
		{name: "beyond the int64 range", value: 1e20, want: 1e20},
		{name: "positive infinity", value: math.Inf(1), want: math.Inf(1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RoundFloat64(tt.value, 4); got != tt.want {
				t.Errorf("RoundFloat64(%v, 4) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestRoundFloat64KeepsNaN(t *testing.T) {
	if got := RoundFloat64(math.NaN(), 4); !math.IsNaN(got) {
		t.Errorf("RoundFloat64(NaN, 4) = %v, want NaN", got)
	}
}
