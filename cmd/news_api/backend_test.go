package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/api/server"
	"github.com/DjordjeVuckovic/tusker/internal/embedding"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/storage/es"
	"github.com/DjordjeVuckovic/tusker/internal/storage/factory"
	"github.com/DjordjeVuckovic/tusker/internal/storage/pg"
	pkgtesting "github.com/DjordjeVuckovic/tusker/pkg/testing"
	"github.com/jackc/pgx/v5"
)

func pgSearchConfig(connStr string) *NewsSearchConfig {
	return &NewsSearchConfig{
		StorageConfig: factory.StorageConfig{
			Type: storage.PG,
			Pg:   &pg.PoolConfig{ConnStr: connStr},
		},
		EmbeddingConfig: embedding.Config{Enabled: true, BaseURL: "http://127.0.0.1:1"},
	}
}

func healthStatus(t *testing.T, backend *searchBackend) int {
	t.Helper()
	s, err := server.New(&server.Config{Port: "0"}, backend.health)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	s.SetupHealthChecks("/health")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec.Code
}

func TestOpenSearchBackend_PostgresSearchersShareOnePool(t *testing.T) {
	ctx := context.Background()
	container := pkgtesting.NewPGContainerWithCleanup(ctx, t)

	backend, err := openSearchBackend(ctx, pgSearchConfig(container.ConnString+"&pool_max_conns=1"))
	if err != nil {
		t.Fatalf("openSearchBackend: %v", err)
	}
	t.Cleanup(backend.close)
	if backend.semantic == nil || backend.hybrid == nil {
		t.Fatalf("embeddings enabled but semantic=%v hybrid=%v", backend.semantic, backend.hybrid)
	}

	probe, err := pgx.Connect(ctx, container.ConnString)
	if err != nil {
		t.Fatalf("connect probe: %v", err)
	}
	t.Cleanup(func() { _ = probe.Close(ctx) })

	var connections int
	err = probe.QueryRow(ctx, `
		SELECT count(*) FROM pg_stat_activity
		WHERE datname = current_database()
		  AND backend_type = 'client backend'
		  AND pid <> pg_backend_pid()`).Scan(&connections)
	if err != nil {
		t.Fatalf("count connections: %v", err)
	}

	if connections > 1 {
		t.Errorf("API holds %d connections with pool_max_conns=1; searchers do not share one pool", connections)
	}
}

func TestOpenSearchBackend_HealthFollowsPostgres(t *testing.T) {
	ctx := context.Background()
	container := pkgtesting.NewPGContainerWithCleanup(ctx, t)

	running, err := openSearchBackend(ctx, pgSearchConfig(container.ConnString))
	if err != nil {
		t.Fatalf("openSearchBackend: %v", err)
	}
	t.Cleanup(running.close)
	closed, err := openSearchBackend(ctx, pgSearchConfig(container.ConnString))
	if err != nil {
		t.Fatalf("openSearchBackend: %v", err)
	}

	if got := healthStatus(t, running); got != http.StatusOK {
		t.Fatalf("/health with Postgres up = %d, want %d", got, http.StatusOK)
	}

	closed.close()
	if got := healthStatus(t, closed); got == http.StatusOK {
		t.Errorf("/health after the backend closed = %d, want non-200", got)
	}

	stopTimeout := 5 * time.Second
	if err := container.Container.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop postgres: %v", err)
	}
	if got := healthStatus(t, running); got == http.StatusOK {
		t.Errorf("/health with Postgres stopped = %d, want non-200", got)
	}
}

func TestOpenSearchBackend_HealthFollowsElasticsearch(t *testing.T) {
	reachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(reachable.Close)

	tests := []struct {
		name    string
		address string
		healthy bool
	}{
		{name: "cluster answers", address: reachable.URL, healthy: true},
		{name: "nothing listening", address: unusedAddress(t), healthy: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &NewsSearchConfig{StorageConfig: factory.StorageConfig{
				Type: storage.ES,
				Es:   &es.ClientConfig{Addresses: []string{tt.address}, IndexName: "articles"},
			}}
			backend, err := openSearchBackend(context.Background(), cfg)
			if err != nil {
				t.Fatalf("openSearchBackend: %v", err)
			}
			t.Cleanup(backend.close)

			got := healthStatus(t, backend)
			if tt.healthy && got != http.StatusOK {
				t.Errorf("/health = %d, want %d", got, http.StatusOK)
			}
			if !tt.healthy && got == http.StatusOK {
				t.Errorf("/health = %d, want non-200", got)
			}
		})
	}
}

func unusedAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := "http://" + ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return address
}
