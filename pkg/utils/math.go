package utils

import "math"

// RoundFloat64 rounds a float64 value to the specified number of decimal places,
// half away from zero. For example, RoundFloat64(3.14159, 2) returns 3.14.
func RoundFloat64(value float64, decimals int) float64 {
	pow := math.Pow10(decimals)
	return math.Round(value*pow) / pow
}
