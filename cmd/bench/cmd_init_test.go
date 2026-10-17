package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/bench/runner"
	"github.com/DjordjeVuckovic/tusker/internal/bench/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInit_ScaffoldedSpecResolvesItsSuite(t *testing.T) {
	tests := []struct {
		name      string
		trackRoot func(cwd string) string
		trackDir  string
	}{
		{name: "track root defaults to cwd", trackRoot: func(string) string { return "" }, trackDir: "cc-news/fts"},
		{name: "relative track root", trackRoot: func(string) string { return "tracks" }, trackDir: "tracks/cc-news/fts"},
		{name: "absolute track root", trackRoot: func(cwd string) string { return cwd }, trackDir: "cc-news/fts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cwd := t.TempDir()
			t.Chdir(cwd)
			prevRoot := trackRoot
			trackRoot = tt.trackRoot(cwd)
			t.Cleanup(func() { trackRoot = prevRoot })

			var out bytes.Buffer
			cmd := newInitCmd()
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{"cc-news/fts"})
			require.NoError(t, cmd.Execute())

			bs, err := spec.LoadFromFile(filepath.Join(cwd, tt.trackDir, "spec.yaml"))
			require.NoError(t, err)
			require.NotEmpty(t, bs.Jobs)

			_, err = runner.LoadSuites(bs)
			require.NoError(t, err)
			for name, eng := range bs.Engines {
				if eng.Type == spec.EnginePostgres {
					assert.Equal(t, "force_custom_plan", eng.ConnectionSettings["plan_cache_mode"], name)
				}
			}
		})
	}
}
