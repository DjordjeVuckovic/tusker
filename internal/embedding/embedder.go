package embedding

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"github.com/google/uuid"
)

// DefaultDim is Qwen3-Embedding-0.6B's native width.
const DefaultDim = 1024

// Embedder slices every vector to its width, L2-normalises it, and tags it model@dim.
type Embedder struct {
	model string
	dim   int

	client Client
}

type Vec struct {
	Embedding []float32
	Model     string
	ID        uuid.UUID
}

type EmbedderOption func(executor *Embedder)

func NewEmbedder(client Client, opts ...EmbedderOption) *Embedder {
	base := &Embedder{
		model:  DefaultModel,
		dim:    DefaultDim,
		client: client,
	}

	for _, opt := range opts {
		opt(base)
	}

	return base
}

func WithExecutorModel(model string) EmbedderOption {
	return func(executor *Embedder) {
		executor.model = model
	}
}

func WithDim(dim int) EmbedderOption {
	return func(executor *Embedder) {
		executor.dim = dim
	}
}

// Model returns the model@dim tag, e.g. qwen3-embedding:0.6b@1024.
func (e *Embedder) Model() string {
	return fmt.Sprintf("%s@%d", e.model, e.dim)
}

func (e *Embedder) fit(raw []float32) ([]float32, error) {
	if len(raw) < e.dim {
		return nil, fmt.Errorf("model %s returned %d dimensions, want at least %d", e.model, len(raw), e.dim)
	}
	var sumSquares float64
	for _, v := range raw[:e.dim] {
		sumSquares += float64(v) * float64(v)
	}
	norm := math.Sqrt(sumSquares)
	if norm == 0 || math.IsNaN(norm) || math.IsInf(norm, 0) {
		return nil, fmt.Errorf("model %s returned a vector with norm %v, which cannot be normalised", e.model, norm)
	}
	fitted := make([]float32, e.dim)
	for i, v := range raw[:e.dim] {
		fitted[i] = float32(float64(v) / norm)
	}
	return fitted, nil
}

func (e *Embedder) EmbedQuery(ctx context.Context, query string) (*Vec, error) {
	task := "Given a web search, retrieve all relevant news documents"
	instruct := wrapWithInstruct(
		task,
		strings.TrimSpace(query),
	)

	slog.Debug("embedding query with instruct", "task", task, "query", query)

	embed, err := e.client.Generate(ctx, Request{
		Model:  e.model,
		Prompt: instruct,
	})
	if err != nil {
		return nil, err
	}

	fitted, err := e.fit(embed.Embedding)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	return &Vec{
		Embedding: fitted,
		Model:     e.Model(),
		ID:        uuid.Nil,
	}, nil
}

func wrapWithInstruct(task, query string) string {
	return fmt.Sprintf("Instruct: %s\nQuery:%s", task, query)
}
