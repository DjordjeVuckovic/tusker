package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
)

type APIExecutor struct {
	name    string
	baseURL string
	client  *http.Client
}

func NewAPIExecutor(name, baseURL string) *APIExecutor {
	return &APIExecutor{
		name:    name,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

type apiRequest struct {
	Method string            `json:"method"`
	Path   string            `json:"path"`
	Params map[string]string `json:"params,omitempty"`
	Body   string            `json:"body,omitempty"`
}

func (e *APIExecutor) Execute(ctx context.Context, rawQuery string, _ []any) (*Execution, error) {
	var req apiRequest
	if err := json.Unmarshal([]byte(rawQuery), &req); err != nil {
		return nil, fmt.Errorf("api parse request descriptor: %w", err)
	}

	reqURL := e.baseURL + req.Path

	if len(req.Params) > 0 {
		params := url.Values{}
		for k, v := range req.Params {
			params.Set(k, v)
		}
		reqURL += "?" + params.Encode()
	}

	var bodyReader io.Reader
	if req.Body != "" {
		bodyReader = bytes.NewBufferString(req.Body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("api create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("api request: %w", err)
	}
	defer resp.Body.Close()
	latency := time.Since(start)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("api read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api status %d: %s", resp.StatusCode, string(body))
	}

	var searchResp apiSearchResponse
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return nil, fmt.Errorf("api parse response: %w", err)
	}

	ids := make([]uuid.UUID, 0, len(searchResp.Hits))
	for _, hit := range searchResp.Hits {
		ids = append(ids, hit.Article.ID)
	}

	var corpusMatches *int64
	if searchResp.TotalMatches > 0 {
		corpusMatches = &searchResp.TotalMatches
	}

	return &Execution{
		RankedDocIDs:  ids,
		CorpusMatches: corpusMatches,
		Latency:       latency,
	}, nil
}

func (e *APIExecutor) Name() string { return e.name }
func (e *APIExecutor) Close() error { return nil }

// Validate parses the request descriptor. We can't validate against the live
// API without firing a real request, so this just checks shape: method, path,
// optional body/params.
func (e *APIExecutor) Validate(_ context.Context, rawQuery string) error {
	var req apiRequest
	if err := json.Unmarshal([]byte(rawQuery), &req); err != nil {
		return fmt.Errorf("api descriptor: %w", err)
	}
	if req.Method == "" {
		return fmt.Errorf("api descriptor missing 'method'")
	}
	if req.Path == "" {
		return fmt.Errorf("api descriptor missing 'path'")
	}
	return nil
}

type apiSearchResponse struct {
	TotalMatches int64          `json:"total_matches"`
	Hits         []apiSearchHit `json:"hits"`
}

type apiSearchHit struct {
	Article apiArticle `json:"article"`
}

type apiArticle struct {
	ID uuid.UUID `json:"id"`
}
