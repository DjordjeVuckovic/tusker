package datapackage_test

import (
	"strings"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/datapackage"
)

func TestVersionMatchesIndependentVectors(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		inputs []string
		want   string
	}{
		{"split after two bytes", "article-v2", []string{"ab", "c"}, "article-v2-601d5476"},
		{"split after one byte", "article-v2", []string{"a", "bc"}, "article-v2-3fafa1cf"},
		{
			"embeddings inputs",
			"lead-v1-1024",
			[]string{"sha256:" + strings.Repeat("0", 64), "abc123", "lead-v1", "1024", "512"},
			"lead-v1-1024-938b2254",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := datapackage.Version(tt.prefix, tt.inputs...); got != tt.want {
				t.Errorf("Version() = %q, want %q", got, tt.want)
			}
		})
	}
}
