package embedding

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/google/uuid"
)

// lengthClient embeds a text as {1, len(text)}, so a vector, even normalised,
// can be traced back to the text it came from by the ratio of its components.
type lengthClient struct{}

func (lengthClient) Generate(_ context.Context, req Request) (*Response, error) {
	return &Response{Embedding: []float32{1, float32(len(req.Prompt))}}, nil
}

func (lengthClient) GenerateBatch(_ context.Context, req BatchRequest) (*BatchResponse, error) {
	embeddings := make([][]float32, 0, len(req.Prompts))
	for _, prompt := range req.Prompts {
		embeddings = append(embeddings, []float32{1, float32(len(prompt))})
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
	embedder := NewEmbedder(lengthClient{}, WithDim(2))
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
		got[v.ID] = float32(math.Round(float64(v.Embedding[1] / v.Embedding[0])))
	}
	want := map[uuid.UUID]float32{first: 3, last: 2}
	if len(got) != len(want) || got[first] != want[first] || got[last] != want[last] {
		t.Errorf("vectors by article = %v, want %v: each vector belongs to its own article's text", got, want)
	}
}

// fakeOllama answers /api/embeddings with vector and records the model each
// request named.
func fakeOllama(t *testing.T, vector []float32) (*OllamaClient, *[]string) {
	t.Helper()
	var models []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		models = append(models, body.Model)
		_ = json.NewEncoder(w).Encode(map[string]any{"embedding": vector})
	}))
	t.Cleanup(server.Close)
	client, err := NewOllamaClient(server.URL)
	if err != nil {
		t.Fatalf("NewOllamaClient: %v", err)
	}
	return client, &models
}

func TestEmbedQuery_AsksOllamaForThePlainModelAndTagsTheVectorWithItsWidth(t *testing.T) {
	native := make([]float32, 1100)
	for i := range native {
		native[i] = 1
	}
	client, models := fakeOllama(t, native)

	vec, err := NewEmbedder(client).EmbedQuery(context.Background(), "climate")
	if err != nil {
		t.Fatalf("EmbedQuery: %v", err)
	}

	if len(*models) != 1 || (*models)[0] != "qwen3-embedding:0.6b" {
		t.Errorf("Ollama asked for %q, want the plain model name", *models)
	}
	if vec.Model != "qwen3-embedding:0.6b@1024" {
		t.Errorf("vector tagged %q, want qwen3-embedding:0.6b@1024", vec.Model)
	}
	if len(vec.Embedding) != 1024 {
		t.Errorf("vector width = %d, want 1024", len(vec.Embedding))
	}
}

func TestEmbedQuery_SlicesToTheConfiguredWidthAndRenormalises(t *testing.T) {
	client, _ := fakeOllama(t, []float32{3, 4, 12})

	vec, err := NewEmbedder(client, WithDim(2)).EmbedQuery(context.Background(), "climate")
	if err != nil {
		t.Fatalf("EmbedQuery: %v", err)
	}

	want := []float32{0.6, 0.8}
	if len(vec.Embedding) != len(want) ||
		math.Abs(float64(vec.Embedding[0]-want[0])) > 1e-6 || math.Abs(float64(vec.Embedding[1]-want[1])) > 1e-6 {
		t.Errorf("embedding = %v, want %v", vec.Embedding, want)
	}
	if vec.Model != "qwen3-embedding:0.6b@2" {
		t.Errorf("vector tagged %q, want qwen3-embedding:0.6b@2", vec.Model)
	}
}

func TestEmbedQuery_RefusesAVectorItCannotGiveTheDeclaredWidth(t *testing.T) {
	tests := []struct {
		name   string
		vector []float32
	}{
		{name: "narrower than the width", vector: []float32{1, 2, 3}},
		{name: "zero vector", vector: []float32{0, 0, 0, 0}},
		{name: "not finite", vector: []float32{1, float32(math.NaN()), 0, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := fakeOllama(t, tt.vector)

			vec, err := NewEmbedder(client, WithDim(4)).EmbedQuery(context.Background(), "climate")

			if err == nil {
				t.Errorf("EmbedQuery returned %v, want an error", vec)
			}
		})
	}
}
