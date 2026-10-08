package es

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	dquery "github.com/DjordjeVuckovic/tusker/internal/types/query"
)

func newTestClient(t *testing.T, config ClientConfig) *Client {
	t.Helper()
	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

// unresponsiveCluster accepts connections and never answers, the way a paused
// or wedged Elasticsearch node does.
func unresponsiveCluster(t *testing.T) (address string, accepted *atomic.Int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	accepted = &atomic.Int32{}
	done := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			go func() {
				<-done
				_ = conn.Close()
			}()
		}
	}()
	t.Cleanup(func() {
		close(done)
		_ = ln.Close()
	})
	return "http://" + ln.Addr().String(), accepted
}

func TestSearcher_UnresponsiveClusterFailsInsteadOfHanging(t *testing.T) {
	address, accepted := unresponsiveCluster(t)
	searcher := NewSearcher(newTestClient(t, ClientConfig{
		Addresses:             []string{address},
		IndexName:             "articles",
		ResponseHeaderTimeout: 100 * time.Millisecond,
	}))

	result := make(chan error, 1)
	go func() {
		_, err := searcher.SearchStringQuery(context.Background(), dquery.NewQueryString("climate"), &dquery.BaseOptions{Size: 10})
		result <- err
	}()

	// The guard only stops a hanging client from hanging the test.
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("search against an unresponsive cluster succeeded, want an error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("search against an unresponsive cluster never returned")
	}

	if got := accepted.Load(); got != 1 {
		t.Errorf("client opened %d connections, want 1: a timed-out request must not be retried", got)
	}
}
