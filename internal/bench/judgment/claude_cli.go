package judgment

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

const (
	defaultCLIBinary      = "claude"
	cliPreferredBatchSize = 10
)

// ClaudeCLIStrategy invokes the `claude -p <prompt>` CLI per (query, doc).
// One subprocess per call, focused single-doc context — keeps the model from
// drowning in a 13k-line enriched-pool YAML.
type ClaudeCLIStrategy struct {
	binary string
	model  string
}

func NewClaudeCLIStrategy(opts StrategyOptions) *ClaudeCLIStrategy {
	bin := opts.CLIBinary
	if bin == "" {
		bin = defaultCLIBinary
	}
	model := opts.Model
	if model == "" {
		model = DefaultJudgeModel
	}
	return &ClaudeCLIStrategy{binary: bin, model: model}
}

func (s *ClaudeCLIStrategy) Name() string { return string(StrategyClaudeCLI) }

// ModelID returns the model id passed to `claude --model`. Because the model is
// pinned on every invocation rather than inherited from the CLI's ambient
// configuration, meta.JudgeModel names the model that actually graded — and the
// resume guard can reject a mid-run model swap.
func (s *ClaudeCLIStrategy) ModelID() string { return s.model }

func (s *ClaudeCLIStrategy) Grade(ctx context.Context, q GradingQuery, doc GradingDoc) (int, error) {
	out, err := s.runCLI(ctx, BuildGradingPrompt(q, doc))
	if err != nil {
		return 0, err
	}
	grade, err := ParseGradeJSON(out, doc.ID.String())
	if err != nil {
		return 0, fmt.Errorf("doc %s: %w", doc.ID, err)
	}
	return grade, nil
}

func (s *ClaudeCLIStrategy) PreferredBatchSize() int { return cliPreferredBatchSize }

// GradeBatch implements BatchStrategy. The CLI uses the same prompt structure
// as the API path, but the system prompt is prepended to the user payload —
// `claude -p` only accepts a single positional message.
func (s *ClaudeCLIStrategy) GradeBatch(ctx context.Context, q GradingQuery, docs []GradingDoc) ([]GradedDoc, error) {
	if len(docs) == 0 {
		return nil, nil
	}
	prompt := BatchSystemPrompt + "\n\n" + BuildBatchGradingPrompt(q, docs)
	out, err := s.runCLI(ctx, prompt)
	if err != nil {
		return nil, err
	}
	parsed, missing, err := ParseBatchGradeJSON(out, docs)
	if err != nil {
		return nil, fmt.Errorf("batch parse: %w", err)
	}
	if len(missing) > 0 {
		return parsed, &PartialBatchError{Missing: missing, Got: len(parsed), Want: len(docs)}
	}
	return parsed, nil
}

func (s *ClaudeCLIStrategy) runCLI(ctx context.Context, prompt string) (string, error) {
	cmd := exec.CommandContext(ctx, s.binary, "--model", s.model, "-p", prompt)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("%s --model %s -p exit %d: %s",
				s.binary, s.model, exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return "", fmt.Errorf("%s --model %s -p: %w", s.binary, s.model, err)
	}
	return string(out), nil
}
