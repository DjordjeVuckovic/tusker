package embedding

import (
	"context"
	"errors"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/google/uuid"
)

// lengthClient embeds a text as a one-element vector holding its length, so a
// vector can be traced back to the text it came from.
type lengthClient struct{}

func (lengthClient) Generate(_ context.Context, req Request) (*Response, error) {
	return &Response{Embedding: []float32{float32(len(req.Prompt))}}, nil
}

func (lengthClient) GenerateBatch(_ context.Context, req BatchRequest) (*BatchResponse, error) {
	embeddings := make([][]float32, 0, len(req.Prompts))
	for _, prompt := range req.Prompts {
		embeddings = append(embeddings, []float32{float32(len(prompt))})
	}
	return &BatchResponse{Embeddings: embeddings}, nil
}

func TestMapDocToPrompt_MatchesColabRecipe(t *testing.T) {
	tests := []struct {
		name    string
		article document.Article
		want    string
	}{
		{
			name:    "title and description joined by one space",
			article: document.Article{Title: "Title", Description: "Desc"},
			want:    "Title Desc",
		},
		{
			name:    "description missing",
			article: document.Article{Title: "Title"},
			want:    "Title",
		},
		{
			name:    "title missing",
			article: document.Article{Description: "Desc"},
			want:    "Desc",
		},
		{
			name:    "whitespace-only description dropped",
			article: document.Article{Title: "Title", Description: " \n\t"},
			want:    "Title",
		},
		{
			name:    "edges stripped, inner whitespace kept",
			article: document.Article{Title: " Title  A \n", Description: "\tDesc  B "},
			want:    "Title  A Desc  B",
		},
		{
			name:    "python whitespace separators stripped",
			article: document.Article{Title: "\x1cTitle\x1f", Description: "Desc"},
			want:    "Title Desc",
		},
		{
			name:    "content excluded",
			article: document.Article{Title: "Title", Description: "Desc", Content: "Body"},
			want:    "Title Desc",
		},
		{
			name:    "all empty",
			article: document.Article{Content: "Body"},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapDocToPrompt(tt.article); got != tt.want {
				t.Errorf("mapDocToPrompt() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEmbedDoc_ArticleWithoutTitleOrDescriptionIsNotEmbedded(t *testing.T) {
	embedder := NewEmbedder(lengthClient{})

	vec, err := embedder.EmbedDoc(context.Background(), document.Article{Title: " ", Content: "Body"})

	if !errors.Is(err, ErrNothingToEmbed) {
		t.Fatalf("err = %v, want ErrNothingToEmbed", err)
	}
	if vec != nil {
		t.Errorf("vec = %+v, want none", vec)
	}
}

func TestEmbedDocs_SkipsArticlesWithNothingToEmbed(t *testing.T) {
	embedder := NewEmbedder(lengthClient{})
	first, empty, last := uuid.New(), uuid.New(), uuid.New()

	vecs, err := embedder.EmbedDocs(context.Background(), []document.Article{
		{ID: first, Title: "abc"},
		{ID: empty, Content: "Body"},
		{ID: last, Title: "de"},
	})
	if err != nil {
		t.Fatalf("EmbedDocs: %v", err)
	}

	got := make(map[uuid.UUID]float32, len(vecs))
	for _, v := range vecs {
		got[v.ID] = v.Embedding[0]
	}
	want := map[uuid.UUID]float32{first: 3, last: 2}
	if len(got) != len(want) || got[first] != want[first] || got[last] != want[last] {
		t.Errorf("vectors by article = %v, want %v: each vector belongs to its own article's text", got, want)
	}
}
