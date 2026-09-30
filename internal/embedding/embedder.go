package embedding

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"unicode"

	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/google/uuid"
)

type Embedder struct {
	maxLength *int
	model     string

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

func WithExecutorMaxLength(length int) EmbedderOption {
	return func(executor *Embedder) {
		executor.maxLength = &length
	}
}

func (e *Embedder) EmbedDoc(ctx context.Context, ar document.Article) (*Vec, error) {
	prompt := mapDocToPrompt(ar)

	slog.Debug("Embedding document", "title", ar.Title, "content_length", len(ar.Content))

	embed, err := e.client.Generate(ctx, Request{
		Model:  e.model,
		Prompt: prompt,
	})
	if err != nil {
		return nil, err
	}

	if e.maxLength != nil && len(embed.Embedding) > *e.maxLength {
		return &Vec{
			Embedding: embed.Embedding[:*e.maxLength],
			Model:     e.model,
			ID:        ar.ID,
		}, nil
	}

	slog.Debug("Generated embedding", "embedding_length", len(embed.Embedding), "model", e.model)
	return &Vec{
		Embedding: embed.Embedding,
		Model:     e.model,
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

	if e.maxLength != nil && len(embed.Embedding) > *e.maxLength {
		return &Vec{
			Embedding: embed.Embedding[:*e.maxLength],
			Model:     e.model,
			ID:        uuid.Nil,
		}, nil
	}

	return &Vec{
		Embedding: embed.Embedding,
		Model:     e.model,
		ID:        uuid.Nil,
	}, nil
}

func (e *Embedder) EmbedDocs(ctx context.Context, docs []document.Article) ([]Vec, error) {
	if len(docs) == 0 {
		return nil, nil
	}

	prompts := make([]string, len(docs))
	for i, doc := range docs {
		prompts[i] = mapDocToPrompt(doc)
	}

	slog.Debug("Bulk embedding documents", "count", len(docs))

	resp, err := e.client.GenerateBatch(ctx, BatchRequest{
		Model:   e.model,
		Prompts: prompts,
	})
	if err != nil {
		return nil, err
	}

	if len(resp.Embeddings) != len(docs) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(docs), len(resp.Embeddings))
	}

	vecs := make([]Vec, len(docs))
	for i, emb := range resp.Embeddings {
		embedding := emb
		if e.maxLength != nil && len(embedding) > *e.maxLength {
			embedding = embedding[:*e.maxLength]
		}

		vecs[i] = Vec{
			Embedding: embedding,
			Model:     e.model,
			ID:        docs[i].ID,
		}
	}

	slog.Debug("Generated bulk embeddings", "count", len(vecs), "model", e.model)
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
