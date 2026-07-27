package judgment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// DefaultJudgeModel is the model every Claude-backed judge uses when --model is
// omitted. Haiku is the default across both transports: grading is a short,
// well-specified classification task, so the cheapest capable model is the right
// baseline, and sharing one default keeps CLI- and API-graded qrels comparable.
const DefaultJudgeModel = "claude-haiku-4-5-20251001"

// claudeModelAliases lets --model take a short name. The alias is expanded at
// construction so the judge block always records a concrete model id — an
// alias would silently re-point at a different model later.
var claudeModelAliases = map[string]string{
	"haiku":  DefaultJudgeModel,
	"sonnet": "claude-sonnet-5",
	"opus":   "claude-opus-5",
	"fable":  "claude-fable-5",
}

const (
	claudeDefaultBinary  = "claude"
	claudeDefaultBaseURL = "https://api.anthropic.com"
	claudeAPIKeyEnv      = "ANTHROPIC_API_KEY"
	claudeAPIVersion     = "2023-06-01"

	claudeCLIBatchSize = 10
	claudeAPIBatchSize = 20
)

// resolveClaudeModel expands an alias, defaults an empty value, and otherwise
// passes a full model id through untouched.
func resolveClaudeModel(model string) string {
	if model == "" {
		return DefaultJudgeModel
	}
	if full, ok := claudeModelAliases[strings.ToLower(model)]; ok {
		return full
	}
	return model
}

// ClaudeModelAliases lists the accepted --model shorthands, sorted, for help
// text and error messages.
func ClaudeModelAliases() []string {
	out := make([]string, 0, len(claudeModelAliases))
	for a := range claudeModelAliases {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// claudeCLIProvider drives `claude --model <id> -p <prompt>` as a subprocess.
type claudeCLIProvider struct {
	binary string
	model  string
}

func newClaudeCLIProvider(opts StrategyOptions) (LLMProvider, error) {
	bin := opts.CLIBinary
	if bin == "" {
		bin = claudeDefaultBinary
	}
	return &claudeCLIProvider{binary: bin, model: resolveClaudeModel(opts.Model)}, nil
}

func (p *claudeCLIProvider) Name() Provider          { return ProviderClaudeCLI }
func (p *claudeCLIProvider) Model() string           { return p.model }
func (p *claudeCLIProvider) PreferredBatchSize() int { return claudeCLIBatchSize }

// Complete prepends the system prompt to the user payload: `claude -p` takes a
// single positional message. The model is pinned on every invocation rather
// than inherited from the CLI's ambient config, so the judge block names the
// model that actually graded.
func (p *claudeCLIProvider) Complete(ctx context.Context, req CompletionRequest) (string, error) {
	prompt := req.User
	if req.System != "" {
		prompt = req.System + "\n\n" + req.User
	}

	out, err := exec.CommandContext(ctx, p.binary, "--model", p.model, "-p", prompt).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("%s --model %s -p exit %d: %s",
				p.binary, p.model, exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return "", fmt.Errorf("%s --model %s -p: %w", p.binary, p.model, err)
	}
	return string(out), nil
}

// claudeAPIProvider drives the Anthropic Messages API.
type claudeAPIProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

func newClaudeAPIProvider(opts StrategyOptions) (LLMProvider, error) {
	key := opts.APIKey
	if key == "" {
		key = os.Getenv(claudeAPIKeyEnv)
	}
	if key == "" {
		return nil, fmt.Errorf("provider %s: %s not set and no key provided", ProviderClaudeAPI, claudeAPIKeyEnv)
	}
	base := opts.APIBaseURL
	if base == "" {
		base = claudeDefaultBaseURL
	}
	return &claudeAPIProvider{
		apiKey:  key,
		model:   resolveClaudeModel(opts.Model),
		baseURL: base,
		client:  &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (p *claudeAPIProvider) Name() Provider          { return ProviderClaudeAPI }
func (p *claudeAPIProvider) Model() string           { return p.model }
func (p *claudeAPIProvider) PreferredBatchSize() int { return claudeAPIBatchSize }

func (p *claudeAPIProvider) Complete(ctx context.Context, cr CompletionRequest) (string, error) {
	payload, err := json.Marshal(messagesRequest{
		Model:     p.model,
		MaxTokens: cr.MaxTokens,
		System:    cr.System,
		Messages:  []messagesEntry{{Role: "user", Content: cr.User}},
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", claudeAPIVersion)
	req.Header.Set("content-type", "application/json")

	resp, err := p.client.Do(req)
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
