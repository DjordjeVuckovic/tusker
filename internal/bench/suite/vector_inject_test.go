package suite

import (
	"strings"
	"testing"
)

func TestFormatVector(t *testing.T) {
	got := FormatVector([]float32{0.1, 0.2, -0.3})
	if got != "[0.1,0.2,-0.3]" {
		t.Errorf("FormatVector = %q, want %q", got, "[0.1,0.2,-0.3]")
	}
	if got := FormatVector(nil); got != "[]" {
		t.Errorf("FormatVector(nil) = %q, want []", got)
	}
}

func TestNeedsQueryVector(t *testing.T) {
	withParam := Query{Engines: map[string]EngineQuery{
		"pg": {Template: "pgvec", Params: TemplateParams{"embedding": "{{precomputed}}"}},
	}}
	withInline := Query{Engines: map[string]EngineQuery{
		"es": {Query: `{"knn":{"query_vector": {{precomputed}}}}`},
	}}
	without := Query{Engines: map[string]EngineQuery{
		"pg": {Query: "SELECT id FROM articles"},
	}}

	if !withParam.NeedsQueryVector() {
		t.Error("query with {{precomputed}} param should need a vector")
	}
	if !withInline.NeedsQueryVector() {
		t.Error("inline query referencing {{precomputed}} should need a vector")
	}
	if without.NeedsQueryVector() {
		t.Error("plain query should not need a vector")
	}
}

func TestResolveEngineQuery_InjectsTemplateVector(t *testing.T) {
	reg := NewTemplateRegistry()
	if err := reg.Register(&QueryTemplate{ID: "pgvec", Query: "ORDER BY embedding <=> '{{embedding}}'::vector LIMIT {{limit}}"}); err != nil {
		t.Fatal(err)
	}
	q := Query{Engines: map[string]EngineQuery{
		"pg": {Template: "pgvec", Params: TemplateParams{"embedding": "{{precomputed}}", "limit": 50}},
	}}
	extra := TemplateParams{ReservedQueryVectorParam: "[1,2,3]"}

	resolved, err := q.ResolveEngineQuery("pg", reg, "", extra)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !strings.Contains(resolved.Query, "'[1,2,3]'::vector") {
		t.Errorf("expected injected vector, got %q", resolved.Query)
	}
}

func TestResolveEngineQuery_InjectsInlineVector(t *testing.T) {
	q := Query{Engines: map[string]EngineQuery{
		"es": {Query: `{"knn":{"field":"embedding","query_vector": {{precomputed}}}}`},
	}}
	extra := TemplateParams{ReservedQueryVectorParam: "[0.5,0.5]"}

	resolved, err := q.ResolveEngineQuery("es", nil, "", extra)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !strings.Contains(resolved.Query, `"query_vector": [0.5,0.5]`) {
		t.Errorf("expected injected JSON array, got %q", resolved.Query)
	}
}

func TestResolveEngineQuery_UnresolvedInlinePlaceholderErrors(t *testing.T) {
	q := Query{Engines: map[string]EngineQuery{
		"es": {Query: `{"knn":{"field":"embedding","query_vector": {{precomputed}}}}`},
	}}

	// No extra params: {{precomputed}} stays unresolved and must error instead of
	// shipping a literal "{" to the engine.
	_, err := q.ResolveEngineQuery("es", nil, "", nil)
	if err == nil {
		t.Fatal("expected error for unresolved inline placeholder, got nil")
	}
	if !strings.Contains(err.Error(), "precomputed") {
		t.Errorf("error should name the missing placeholder, got %q", err.Error())
	}
}
