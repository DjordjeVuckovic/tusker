package es

import (
	"context"

	"github.com/elastic/go-elasticsearch/v8"
)

type HealthChecker struct {
	client *elasticsearch.TypedClient
}

func NewHealthChecker(client *Client) *HealthChecker {
	return &HealthChecker{client: client.typed}
}

func (hc *HealthChecker) Healthy(ctx context.Context) bool {
	ok, err := hc.client.Ping().Do(ctx)
	return err == nil && ok
}
