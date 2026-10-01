package testing

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
)

func TestNewPGContainer_StartsTheConfiguredEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainer-backed test in -short mode")
	}

	engines := []struct {
		name      string
		engine    Engine
		extension string
	}{
		{name: "native", engine: EngineNative, extension: "pg_trgm"},
		{name: "paradedb", engine: EngineParadeDB, extension: "pg_search"},
		{name: "tiger", engine: EngineTiger, extension: "pg_textsearch"},
	}
	for _, tt := range engines {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			container, err := NewPGContainer(ctx, PGConfig{
				Engine:   tt.engine,
				Database: "engine_test_db",
				Username: "test",
				Password: "test",
			})
			if err != nil {
				t.Fatalf("NewPGContainer: %v", err)
			}
			t.Cleanup(func() { _ = testcontainers.TerminateContainer(container.Container) })

			conn, err := pgx.Connect(ctx, container.ConnString)
			if err != nil {
				t.Fatalf("connect: %v", err)
			}
			defer conn.Close(ctx)

			var installed bool
			err = conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = $1)`, tt.extension).Scan(&installed)
			if err != nil {
				t.Fatalf("query extensions: %v", err)
			}
			if !installed {
				t.Errorf("extension %s is not installed, so the container is not %s", tt.extension, tt.name)
			}
		})
	}
}
