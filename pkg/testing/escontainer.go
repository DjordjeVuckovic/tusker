package testing

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/elasticsearch"
	"github.com/testcontainers/testcontainers-go/wait"
)

// ESContainer represents a running Elasticsearch test container
type ESContainer struct {
	Container testcontainers.Container
	Address   string
}

// NewESContainer starts an Elasticsearch test container
func NewESContainer(ctx context.Context, tb testing.TB) *ESContainer {
	tb.Helper()
	if testing.Short() {
		tb.Skip("skipping testcontainer-backed test in -short mode")
	}

	// Elasticsearch 8 turns on security, and with it TLS, by default. The
	// readiness probe below speaks plaintext, so without this the node comes up
	// healthy, answers every poll with "received plaintext http traffic on an
	// https channel", and the wait times out.
	esContainer, err := elasticsearch.Run(ctx,
		"docker.elastic.co/elasticsearch/elasticsearch:8.12.0",
		testcontainers.WithEnv(map[string]string{
			"xpack.security.enabled": "false",
			"discovery.type":         "single-node",
		}),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/").
				WithPort("9200").
				WithStartupTimeout(180*time.Second),
		),
	)
	if err != nil {
		tb.Fatalf("failed to start elasticsearch container: %v", err)
	}

	tb.Cleanup(func() {
		if err := testcontainers.TerminateContainer(esContainer); err != nil {
			tb.Logf("failed to terminate elasticsearch container: %v", err)
		}
	})

	host, err := esContainer.Host(ctx)
	if err != nil {
		tb.Fatalf("failed to get elasticsearch host: %v", err)
	}

	port, err := esContainer.MappedPort(ctx, "9200")
	if err != nil {
		tb.Fatalf("failed to get elasticsearch port: %v", err)
	}

	address := fmt.Sprintf("http://%s:%s", host, port.Port())

	return &ESContainer{
		Container: esContainer,
		Address:   address,
	}
}
