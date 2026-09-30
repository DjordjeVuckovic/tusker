package es

import (
	"context"
	"fmt"

	"github.com/elastic/go-elasticsearch/v8"
)

type HealthChecker struct {
	client *elasticsearch.TypedClient
}

func NewHealthChecker(config ClientConfig) (*HealthChecker, error) {
	client, err := newClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}
	return &HealthChecker{client: client}, nil
}

func (hc *HealthChecker) Healthy(ctx context.Context) bool {
	ok, err := hc.client.Ping().Do(ctx)
	return err == nil && ok
}
