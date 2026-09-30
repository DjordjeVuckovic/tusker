package embedding

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode"

	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/google/uuid"
)

// ErrNothingToEmbed marks an article whose title and description are both empty.
// Its vector would be the same for every such article and match any query alike.
var ErrNothingToEmbed = errors.New("article has no title or description to embed")

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
		embedding := emb
		if e.maxLength != nil && len(embedding) > *e.maxLength {
			embedding = embedding[:*e.maxLength]
		}

		vecs[i] = Vec{
			Embedding: embedding,
			Model:     e.model,
			ID:        embeddable[i].ID,
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
