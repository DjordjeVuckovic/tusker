package main

import (
	"context"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/embedding"
)

type recordingClient struct {
	models []string
}

func (c *recordingClient) Generate(_ context.Context, req embedding.Request) (*embedding.Response, error) {
	c.models = append(c.models, req.Model)
	return &embedding.Response{Embedding: []float32{1, 0}}, nil
}

func (c *recordingClient) GenerateBatch(context.Context, embedding.BatchRequest) (*embedding.BatchResponse, error) {
	return &embedding.BatchResponse{}, nil
}

func TestNewQueryEmbedder_EmbedsWithConfiguredModel(t *testing.T) {
	tests := []struct {
		name      string
		cfg       embedding.Config
		wantModel string
	}{
		{name: "configured model", cfg: embedding.Config{Model: "bge-m3"}, wantModel: "bge-m3"},
		{name: "no model configured", cfg: embedding.Config{}, wantModel: embedding.DefaultModel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &recordingClient{}

			vec, err := newQueryEmbedder(client, tt.cfg).EmbedQuery(context.Background(), "climate")
			if err != nil {
				t.Fatalf("EmbedQuery: %v", err)
			}

			if len(client.models) != 1 || client.models[0] != tt.wantModel {
				t.Errorf("Ollama asked for %v, want %q", client.models, tt.wantModel)
			}
			if vec.Model != tt.wantModel {
				t.Errorf("query vector tagged %q, want %q", vec.Model, tt.wantModel)
			}
		})
	}
}
