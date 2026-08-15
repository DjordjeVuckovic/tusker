package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type EsExecutor struct {
	name    string
	baseURL string
	index   string
	client  *http.Client
}

func NewEsExecutor(name, baseURL, index string) *EsExecutor {
	return &EsExecutor{
		name:    name,
		baseURL: baseURL,
		index:   index,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (e *EsExecutor) Execute(ctx context.Context, rawQuery string, _ []any) (*Execution, error) {
	url := fmt.Sprintf("%s/%s/_search", e.baseURL, e.index)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBufferString(rawQuery))
	if err != nil {
		return nil, fmt.Errorf("es create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("es request: %w", err)
	}
	defer resp.Body.Close()
	latency := time.Since(start)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("es read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("es status %d: %s", resp.StatusCode, string(body))
	}

	var esResp esSearchResponse
	if err := json.Unmarshal(body, &esResp); err != nil {
		return nil, fmt.Errorf("es parse response: %w", err)
	}

	ids := make([]uuid.UUID, 0, len(esResp.Hits.Hits))
	for _, hit := range esResp.Hits.Hits {
		id, err := uuid.Parse(hit.Source.ID)
		if err != nil {
			return nil, fmt.Errorf("es parse doc id %q: %w", hit.Source.ID, err)
		}
		ids = append(ids, id)
	}

	return &Execution{
		RankedDocIDs:  ids,
		CorpusMatches: esResp.Hits.Total.exactCount(),
		Latency:       latency,
	}, nil
}

func (e *EsExecutor) CorpusCount(ctx context.Context) (int64, error) {
	url := fmt.Sprintf("%s/%s/_count", e.baseURL, e.index)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("es count request: %w", err)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("es count http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("es count read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("es count status %d: %s", resp.StatusCode, string(body))
	}
	var c struct {
		Count int64 `json:"count"`
	}
	if err := json.Unmarshal(body, &c); err != nil {
		return 0, fmt.Errorf("es count parse: %w", err)
	}
	return c.Count, nil
}

func (e *EsExecutor) Name() string { return e.name }
func (e *EsExecutor) Close() error { return nil }

// Validate posts the query to <index>/_validate/query?explain=true. ES parses
// the JSON, type-checks fields, and returns "valid: false" with an explanation
// for malformed queries — no documents scanned.
//
// Bodies that use top-level `knn` (vector search) are routed to
// validateKnnBody: _validate/query only understands the Query DSL and rejects
// `knn` outright ("request does not support [knn]"), so the knn clause is
// checked structurally instead.
func (e *EsExecutor) Validate(ctx context.Context, rawQuery string) error {
	if hasKnn(rawQuery) {
		return e.validateKnnBody(ctx, rawQuery)
	}
	url := fmt.Sprintf("%s/%s/_validate/query?explain=true", e.baseURL, e.index)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBufferString(rawQuery))
	if err != nil {
		return fmt.Errorf("es validate request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("es validate http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("es validate read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("es validate status %d: %s", resp.StatusCode, string(body))
	}

	var v esValidateResponse
	if err := json.Unmarshal(body, &v); err != nil {
		return fmt.Errorf("es validate parse: %w", err)
	}
	if !v.Valid {
		for _, exp := range v.Explanations {
			if exp.Error != "" {
				return fmt.Errorf("es invalid: %s", exp.Error)
			}
		}
		// _validate/query rejects top-level fields like "size","from","aggs".
		// Strip them and retry with just the query block before failing.
		stripped, ok := stripToQueryBody(rawQuery)
		if !ok {
			return fmt.Errorf("es invalid: %s", string(body))
		}
		return e.validateBody(ctx, stripped, body)
	}
	return nil
}

// validateBody is a second-chance validation against a stripped query body
// (just the `query` field). origBody is the original response, used in the
// error message if the retry also fails.
func (e *EsExecutor) validateBody(ctx context.Context, body []byte, origBody []byte) error {
	url := fmt.Sprintf("%s/%s/_validate/query?explain=true", e.baseURL, e.index)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("es validate retry: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("es validate retry http: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var v esValidateResponse
	if err := json.Unmarshal(respBody, &v); err != nil {
		return fmt.Errorf("es invalid: %s", string(origBody))
	}
	if !v.Valid {
		for _, exp := range v.Explanations {
			if exp.Error != "" {
				return fmt.Errorf("es invalid: %s", exp.Error)
			}
		}
		return fmt.Errorf("es invalid: %s", string(respBody))
	}
	return nil
}

// stripToQueryBody extracts just the "query" field from an ES search body —
// _validate/query rejects top-level "size", "from", "aggs", etc.
func stripToQueryBody(raw string) ([]byte, bool) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, false
	}
	q, ok := m["query"]
	if !ok {
		return nil, false
	}
	out, err := json.Marshal(map[string]json.RawMessage{"query": q})
	if err != nil {
		return nil, false
	}
	return out, true
}

// hasKnn reports whether the search body carries a top-level `knn` clause.
func hasKnn(raw string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return false
	}
	_, ok := m["knn"]
	return ok
}

// validateKnnBody validates a vector-search body. The `knn` clause is checked
// structurally (shape, not data — the query_vector may still be an unresolved
// placeholder at validate time); an accompanying `query` block, if present, is
// validated against _validate/query as usual.
func (e *EsExecutor) validateKnnBody(ctx context.Context, rawQuery string) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rawQuery), &m); err != nil {
		return fmt.Errorf("es invalid: body is not JSON: %w", err)
	}
	if err := validateKnnClause(m["knn"]); err != nil {
		return fmt.Errorf("es invalid: %w", err)
	}
	if q, ok := m["query"]; ok {
		body, err := json.Marshal(map[string]json.RawMessage{"query": q})
		if err != nil {
			return fmt.Errorf("es invalid: %w", err)
		}
		return e.validateBody(ctx, body, []byte(rawQuery))
	}
	return nil
}

// validateKnnClause accepts a single knn object or an array of them.
func validateKnnClause(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("knn clause is empty")
	}
	var single map[string]json.RawMessage
	if err := json.Unmarshal(raw, &single); err == nil {
		return validateKnnEntry(single)
	}
	var many []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &many); err != nil {
		return fmt.Errorf("knn must be an object or array of objects")
	}
	if len(many) == 0 {
		return fmt.Errorf("knn array is empty")
	}
	for i, entry := range many {
		if err := validateKnnEntry(entry); err != nil {
			return fmt.Errorf("knn[%d]: %w", i, err)
		}
	}
	return nil
}

func validateKnnEntry(entry map[string]json.RawMessage) error {
	field, ok := entry["field"]
	if !ok {
		return fmt.Errorf("knn missing required \"field\"")
	}
	var fieldName string
	if err := json.Unmarshal(field, &fieldName); err != nil || fieldName == "" {
		return fmt.Errorf("knn \"field\" must be a non-empty string")
	}
	_, hasVec := entry["query_vector"]
	_, hasBuilder := entry["query_vector_builder"]
	if !hasVec && !hasBuilder {
		return fmt.Errorf("knn requires \"query_vector\" or \"query_vector_builder\"")
	}
	return nil
}

type esValidateResponse struct {
	Valid        bool                    `json:"valid"`
	Explanations []esValidateExplanation `json:"explanations,omitempty"`
}

type esValidateExplanation struct {
	Index string `json:"index"`
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
}

type esSearchResponse struct {
	Hits esHits `json:"hits"`
}

type esHits struct {
	Total esTotal `json:"total"`
	Hits  []esHit `json:"hits"`
}

// esTotal is ES hits.total. Relation is "gte" when ES stopped counting at
// track_total_hits rather than counting every match.
type esTotal struct {
	Value    int64  `json:"value"`
	Relation string `json:"relation"`
}

func (t esTotal) exactCount() *int64 {
	if t.Relation == "gte" {
		return nil
	}
	v := t.Value
	return &v
}

type esHit struct {
	Source esSource `json:"_source"`
}

type esSource struct {
	ID string `json:"id"`
}
