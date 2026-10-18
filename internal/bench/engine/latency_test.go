package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const bodyDelay = 200 * time.Millisecond

// slowBodyServer sends the response headers at once and the body bodyDelay
// later, the way a server streams a large result.
func slowBodyServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(bodyDelay)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestHTTPExecutors_LatencyCoversReadingTheBody(t *testing.T) {
	docID := uuid.New().String()
	tests := []struct {
		name     string
		body     string
		executor func(url string) Executor
		query    string
	}{
		{
			name:     "elasticsearch",
			body:     `{"hits": {"total": {"value": 1, "relation": "eq"}, "hits": [{"_id": "` + docID + `", "_source": {"id": "` + docID + `"}}]}}`,
			executor: func(url string) Executor { return NewEsExecutor("es", url, "articles") },
			query:    `{"query": {"match_all": {}}}`,
		},
		{
			name:     "api",
			body:     `{"total_matches": 1, "hits": [{"article": {"id": "` + docID + `"}}]}`,
			executor: func(url string) Executor { return NewAPIExecutor("api", url) },
			query:    `{"method": "GET", "path": "/v1/articles/search", "params": {"query": "climate"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := slowBodyServer(t, tt.body)

			got, err := tt.executor(server.URL).Execute(context.Background(), Request{Query: tt.query})

			require.NoError(t, err)
			assert.GreaterOrEqual(t, got.Latency, bodyDelay)
		})
	}
}
