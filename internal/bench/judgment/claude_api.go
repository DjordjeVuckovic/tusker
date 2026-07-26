package judgment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
)

const (
	defaultAPIBaseURL       = "https://api.anthropic.com"
	apiVersion              = "2023-06-01"
	apiMaxOutputTokens      = 64
	apiBatchMaxOutputTokens = 4096
	apiPreferredBatchSize   = 20
)

// ClaudeAPIStrategy calls the Anthropic Messages API directly.
// Faster and more reliable than spawning a CLI subprocess per doc.
// Requires ANTHROPIC_API_KEY (or opts.APIKey).
type ClaudeAPIStrategy struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

func NewClaudeAPIStrategy(opts StrategyOptions) (*ClaudeAPIStrategy, error) {
	key := opts.APIKey
	if key == "" {
		key = os.Getenv("ANTHROPIC_API_KEY")
	}
	if key == "" {
		return nil, fmt.Errorf("claude-api strategy: ANTHROPIC_API_KEY not set and no key provided")
	}
	model := opts.Model
	if model == "" {
		model = DefaultJudgeModel
	}
	base := opts.APIBaseURL
	if base == "" {
		base = defaultAPIBaseURL
	}
	return &ClaudeAPIStrategy{
		apiKey:  key,
		model:   model,
		baseURL: base,
		client:  &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (s *ClaudeAPIStrategy) Name() string    { return string(StrategyClaudeAPI) }
func (s *ClaudeAPIStrategy) ModelID() string { return s.model }

type messagesRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	System    string          `json:"system,omitempty"`
	Messages  []messagesEntry `json:"messages"`
}

type messagesEntry struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (s *ClaudeAPIStrategy) Grade(ctx context.Context, q GradingQuery, doc GradingDoc) (int, error) {
	text, err := s.callMessages(ctx, systemPrompt, BuildGradingPrompt(q, doc), apiMaxOutputTokens)
	if err != nil {
		return 0, err
	}
	grade, err := ParseGradeJSON(text, doc.ID.String())
	if err != nil {
		return 0, fmt.Errorf("doc %s: %w", doc.ID, err)
	}
	return grade, nil
}

func (s *ClaudeAPIStrategy) PreferredBatchSize() int { return apiPreferredBatchSize }

// GradeBatch implements BatchStrategy. One Messages API call per batch; the
// caller (judgment.Runner) is responsible for chunking and for handling docs
// that come back missing or malformed.
func (s *ClaudeAPIStrategy) GradeBatch(ctx context.Context, q GradingQuery, docs []GradingDoc) ([]GradedDoc, error) {
	if len(docs) == 0 {
		return nil, nil
	}
	text, err := s.callMessages(ctx, BatchSystemPrompt, BuildBatchGradingPrompt(q, docs), apiBatchMaxOutputTokens)
	if err != nil {
		return nil, err
	}
	parsed, missing, err := ParseBatchGradeJSON(text, docs)
	if err != nil {
		return nil, fmt.Errorf("batch parse: %w", err)
	}
	if len(missing) > 0 {
		// Surface partial-success: caller (Runner) will retry the missing IDs
		// individually via Grade(). Returning nil error here would discard
		// the parsed entries; we wrap as a typed sentinel error.
		return parsed, &PartialBatchError{Missing: missing, Got: len(parsed), Want: len(docs)}
	}
	return parsed, nil
}

// PartialBatchError lets the runner distinguish "the LLM returned only part of
// the batch — retry the missing IDs" from a hard transport/parse failure.
type PartialBatchError struct {
	Missing []uuid.UUID
	Got     int
	Want    int
}

func (e *PartialBatchError) Error() string {
	return fmt.Sprintf("batch partial: got %d/%d, %d missing", e.Got, e.Want, len(e.Missing))
}

func (s *ClaudeAPIStrategy) callMessages(ctx context.Context, system, user string, maxTokens int) (string, error) {
	body := messagesRequest{
		Model:     s.model,
		MaxTokens: maxTokens,
		System:    system,
		Messages: []messagesEntry{
			{Role: "user", Content: user},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("x-api-key", s.apiKey)
	req.Header.Set("anthropic-version", apiVersion)
	req.Header.Set("content-type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("api request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read api response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("api status %d: %s", resp.StatusCode, string(raw))
	}

	var mr messagesResponse
	if err := json.Unmarshal(raw, &mr); err != nil {
		return "", fmt.Errorf("parse api response: %w", err)
	}
	if mr.Error != nil {
		return "", fmt.Errorf("api error %s: %s", mr.Error.Type, mr.Error.Message)
	}
	if len(mr.Content) == 0 {
		return "", fmt.Errorf("api response has no content")
	}
	return mr.Content[0].Text, nil
}
