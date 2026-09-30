package embedding

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigFromEnv_MaxLength(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    int
		wantErr bool
	}{
		{name: "unset leaves vectors untruncated", raw: "", want: 0},
		{name: "positive width", raw: "1024", want: 1024},
		{name: "zero would store empty vectors", raw: "0", wantErr: true},
		{name: "negative would panic mid-load", raw: "-1", wantErr: true},
		{name: "not a number", raw: "1k", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("EMBEDDING_SOURCE", string(SourceFile))
			t.Setenv("EMBEDDING_MAX_LENGTH", tt.raw)

			cfg, err := LoadConfigFromEnv()

			if tt.wantErr {
				require.ErrorContains(t, err, "EMBEDDING_MAX_LENGTH")
				return
			}
			require.NoError(t, err)
			if tt.want == 0 {
				assert.Nil(t, cfg.MaxLength)
				return
			}
			require.NotNil(t, cfg.MaxLength)
			assert.Equal(t, tt.want, *cfg.MaxLength)
		})
	}
}
