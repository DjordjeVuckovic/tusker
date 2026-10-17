package embedding

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"unicode"

	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/google/uuid"
)

// ErrNothingToEmbed marks an article whose title and description are both empty.
// Its vector would be the same for every such article and match any query alike.
var ErrNothingToEmbed = errors.New("article has no title or description to embed")

// DefaultDim is the width vectors are sliced to when none is configured,
// Qwen3-Embedding-0.6B's native width.
const DefaultDim = 1024

// Embedder embeds text through an Ollama-compatible Client. Ollama is asked
// for the plain model; every vector comes back sliced to the configured width,
// L2-normalised, and tagged model@dim.
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

// Model is the tag stored vectors carry and every vector filter matches:
// the model name with the width, as qwen3-embedding:0.6b@1024.
func (e *Embedder) Model() string {
	return fmt.Sprintf("%s@%d", e.model, e.dim)
}

// fit slices a model output to the embedder's width and L2-normalises it, so
// a vector tagged @dim always has dim components and unit length.
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

func (e *Embedder) EmbedDoc(ctx context.Context, ar document.Article) (*Vec, error) {
	prompt := mapDocToPrompt(ar)
	if prompt == "" {
		return nil, ErrNothingToEmbed
	}

	slog.Debug("Embedding document", "title", ar.Title, "content_length", len(ar.Content))

	embed, err := e.client.Generate(ctx, Request{
		Model:  e.model,
		Prompt: prompt,
	})
	if err != nil {
		return nil, err
	}

	fitted, err := e.fit(embed.Embedding)
	if err != nil {
		return nil, fmt.Errorf("embed article %s: %w", ar.ID, err)
	}
	return &Vec{
		Embedding: fitted,
		Model:     e.Model(),
		ID:        ar.ID,
	}, nil
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

// EmbedDocs skips articles with nothing to embed, so the result can be shorter
// than docs; each Vec carries the ID of its article.
func (e *Embedder) EmbedDocs(ctx context.Context, docs []document.Article) ([]Vec, error) {
	embeddable := make([]document.Article, 0, len(docs))
	prompts := make([]string, 0, len(docs))
	for _, doc := range docs {
		if prompt := mapDocToPrompt(doc); prompt != "" {
			embeddable = append(embeddable, doc)
			prompts = append(prompts, prompt)
		}
	}
	if len(embeddable) == 0 {
		return nil, nil
	}

	slog.Debug("Bulk embedding documents", "count", len(embeddable), "skipped", len(docs)-len(embeddable))

	resp, err := e.client.GenerateBatch(ctx, BatchRequest{
		Model:   e.model,
		Prompts: prompts,
	})
	if err != nil {
		return nil, err
	}

	if len(resp.Embeddings) != len(embeddable) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(embeddable), len(resp.Embeddings))
	}

	vecs := make([]Vec, len(embeddable))
	for i, emb := range resp.Embeddings {
		fitted, err := e.fit(emb)
		if err != nil {
			return nil, fmt.Errorf("embed article %s: %w", embeddable[i].ID, err)
		}
		vecs[i] = Vec{
			Embedding: fitted,
			Model:     e.Model(),
			ID:        embeddable[i].ID,
		}
	}

	slog.Debug("Generated bulk embeddings", "count", len(vecs), "model", e.Model())
	return vecs, nil
}

// mapDocToPrompt must produce the same text as build_text in scripts/embed_corpus.py:
// both paths store vectors under the same model name.
func mapDocToPrompt(ar document.Article) string {
	parts := make([]string, 0, 2)
	for _, field := range []string{ar.Title, ar.Description} {
		if trimmed := strings.TrimFunc(field, isPythonSpace); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, " ")
}

// isPythonSpace matches str.isspace, which also counts the \x1c-\x1f separators.
func isPythonSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

func wrapWithInstruct(task, query string) string {
	return fmt.Sprintf("Instruct: %s\nQuery:%s", task, query)
}
