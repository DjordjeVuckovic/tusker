package engine

import (
	"context"
	"strings"
	"testing"
)

func TestHasKnn(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"knn only", `{"knn":{"field":"embedding","query_vector":[0.1],"k":10}}`, true},
		{"query plus knn", `{"query":{"match_all":{}},"knn":{"field":"e","query_vector":[0.1]}}`, true},
		{"query only", `{"query":{"match_all":{}}}`, false},
		{"not json", `not json`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasKnn(tt.body); got != tt.want {
				t.Errorf("hasKnn(%s) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

func TestValidateKnnClause(t *testing.T) {
	tests := []struct {
		name    string
		clause  string
		wantErr string // substring; "" means no error
	}{
		{"valid object", `{"field":"embedding","query_vector":[0.1,0.2],"k":10}`, ""},
		{"placeholder vector", `{"field":"embedding","query_vector":"{{precomputed}}","k":50}`, ""},
		{"query_vector_builder", `{"field":"embedding","query_vector_builder":{"text_embedding":{}}}`, ""},
		{"valid array", `[{"field":"a","query_vector":[0.1]},{"field":"b","query_vector":[0.2]}]`, ""},
		{"missing field", `{"query_vector":[0.1]}`, "missing required \"field\""},
		{"empty field", `{"field":"","query_vector":[0.1]}`, "non-empty string"},
		{"missing vector", `{"field":"embedding","k":10}`, "requires \"query_vector\""},
		{"empty array", `[]`, "knn array is empty"},
		{"empty clause", ``, "knn clause is empty"},
		{"bad entry in array", `[{"field":"a"}]`, "knn[0]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateKnnClause([]byte(tt.clause))
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

// validateKnnBody on a knn-only body performs no network call (no `query`
// block), so it can be exercised without a live Elasticsearch.
func TestValidateKnnBody_KnnOnly(t *testing.T) {
	e := NewEsExecutor("es", "http://127.0.0.1:1", "articles")
	body := `{"knn":{"field":"embedding","query_vector":"{{precomputed}}","k":50,"num_candidates":200}}`
	if err := e.validateKnnBody(context.Background(), body); err != nil {
		t.Fatalf("expected valid knn body, got: %v", err)
	}

	bad := `{"knn":{"k":50}}`
	if err := e.validateKnnBody(context.Background(), bad); err == nil {
		t.Fatal("expected error for knn body missing field/vector")
	}
}

func TestExactTotal(t *testing.T) {
	tests := []struct {
		name  string
		total esTotal
		want  *int64
	}{
		{"counted every match", esTotal{Value: 2466, Relation: "eq"}, ptr(int64(2466))},
		{"stopped at track_total_hits", esTotal{Value: 10000, Relation: "gte"}, nil},
		{"relation absent", esTotal{Value: 42}, ptr(int64(42))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.total.exactCount()
			switch {
			case tt.want == nil && got != nil:
				t.Fatalf("got %d, want nil", *got)
			case tt.want != nil && got == nil:
				t.Fatalf("got nil, want %d", *tt.want)
			case tt.want != nil && *got != *tt.want:
				t.Fatalf("got %d, want %d", *got, *tt.want)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }
