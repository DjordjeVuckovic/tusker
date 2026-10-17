package env

import (
	"fmt"
	"os"
	"time"
)

// PositiveDuration reads a Go duration such as "30s" from the environment.
// Unset returns 0, so the caller applies its own default; zero, negative or
// malformed values are errors.
func PositiveDuration(name string) (time.Duration, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", name, raw, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("invalid %s %q: must be positive", name, raw)
	}
	return d, nil
}
