package embedding

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeOllama records the model each request names.
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
