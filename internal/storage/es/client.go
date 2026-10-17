package es

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
)

const (
	DefaultDialTimeout = 5 * time.Second
	// DefaultResponseHeaderTimeout covers the slowest call the loaders make:
	// a 5MB bulk flush, which a local node answers in seconds. A slower cluster
	// sets ES_RESPONSE_HEADER_TIMEOUT.
	DefaultResponseHeaderTimeout = 30 * time.Second
)

type ClientConfig struct {
	Addresses []string
	IndexName string
	Username  string
	Password  string

	// DialTimeout and ResponseHeaderTimeout default when zero.
	DialTimeout           time.Duration
	ResponseHeaderTimeout time.Duration
}

// Client is an Elasticsearch connection pool bound to one index. Searchers and
// health checks built from the same Client share its connections.
type Client struct {
	typed     *elasticsearch.TypedClient
	transport *http.Transport
	indexName string
}

func NewClient(config ClientConfig) (*Client, error) {
	transport := newTransport(config)
	typed, err := newTypedClientOn(config, transport)
	if err != nil {
		return nil, fmt.Errorf("create Elasticsearch client: %w", err)
	}
	return &Client{typed: typed, transport: transport, indexName: config.IndexName}, nil
}

// Close releases the client's idle connections. Requests still in flight
// finish on their own connections.
func (c *Client) Close() {
	c.transport.CloseIdleConnections()
}

func newTypedClient(config ClientConfig) (*elasticsearch.TypedClient, error) {
	return newTypedClientOn(config, newTransport(config))
}

func newTypedClientOn(config ClientConfig, transport *http.Transport) (*elasticsearch.TypedClient, error) {
	cfg := elasticsearch.Config{
		Addresses:    config.Addresses,
		Transport:    transport,
		RetryOnError: retryUnlessTimedOut,
	}

	if config.Username != "" && config.Password != "" {
		cfg.Username = config.Username
		cfg.Password = config.Password
	}

	client, err := elasticsearch.NewTypedClient(cfg)

	return client, err
}

func newTransport(config ClientConfig) *http.Transport {
	dialTimeout := config.DialTimeout
	if dialTimeout == 0 {
		dialTimeout = DefaultDialTimeout
	}
	responseHeaderTimeout := config.ResponseHeaderTimeout
	if responseHeaderTimeout == 0 {
		responseHeaderTimeout = DefaultResponseHeaderTimeout
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	return transport
}

// retryUnlessTimedOut keeps the client's retry on connection errors but not on
// timeouts, which would otherwise multiply one timeout by the retry count.
func retryUnlessTimedOut(_ *http.Request, err error) bool {
	var netErr net.Error
	return !errors.As(err, &netErr) || !netErr.Timeout()
}
