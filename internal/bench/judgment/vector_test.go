package judgment

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// fakeVectorStore returns canned vectors so the scoring logic can be tested
// with no embedding backend or database.
type fakeVectorStore struct {
	query []float32
	docs  map[uuid.UUID][]float32
}

func (f fakeVectorStore) QueryVector(context.Context, string) ([]float32, error) {
	return f.query, nil
}

func (f fakeVectorStore) DocVectors(_ context.Context, ids []uuid.UUID) (map[uuid.UUID][]float32, error) {
	out := make(map[uuid.UUID][]float32)
	for _, id := range ids {
		if v, ok := f.docs[id]; ok {
			out[id] = v
		}
	}
	return out, nil
}

func TestVectorGradeBatch(t *testing.T) {
	identical := GradingDoc{ID: uuid.New(), Title: "a"}  // cos 1.0  -> Highly
	related := GradingDoc{ID: uuid.New(), Title: "b"}    // cos 0.6  -> Relevant
	orthogonal := GradingDoc{ID: uuid.New(), Title: "c"} // cos 0.0  -> NotRelev

	store := fakeVectorStore{
		query: []float32{1, 0},
		docs: map[uuid.UUID][]float32{
			identical.ID:  {1, 0},
			related.ID:    {0.6, 0.8},
			orthogonal.ID: {0, 1},
		},
	}
	s := NewVectorStrategyWithStore(store, "fake-model")

	got, err := s.GradeBatch(context.Background(), GradingQuery{ID: "q"}, []GradingDoc{identical, related, orthogonal})
	if err != nil {
		t.Fatalf("GradeBatch: %v", err)
	}
	byID := map[uuid.UUID]int{}
	for _, g := range got {
		byID[g.DocID] = g.Grade
	}
	if byID[identical.ID] != GradeHighly {
		t.Errorf("identical grade = %d, want %d", byID[identical.ID], GradeHighly)
	}
	if byID[related.ID] != GradeRelevant {
		t.Errorf("related grade = %d, want %d", byID[related.ID], GradeRelevant)
	}
	if byID[orthogonal.ID] != GradeNotRelev {
		t.Errorf("orthogonal grade = %d, want %d", byID[orthogonal.ID], GradeNotRelev)
	}
}

func TestVectorGradeBatch_OmitsDocsWithoutVectors(t *testing.T) {
	have := GradingDoc{ID: uuid.New(), Title: "a"}
	missing := GradingDoc{ID: uuid.New(), Title: "b"}
	store := fakeVectorStore{
		query: []float32{1, 0},
		docs:  map[uuid.UUID][]float32{have.ID: {1, 0}}, // missing has no vector
	}
	s := NewVectorStrategyWithStore(store, "fake")

	got, err := s.GradeBatch(context.Background(), GradingQuery{ID: "q"}, []GradingDoc{have, missing})
	if err != nil {
		t.Fatalf("GradeBatch: %v", err)
	}
	// missing doc is omitted so the runner records it as Unjudged.
	if len(got) != 1 || got[0].DocID != have.ID {
		t.Fatalf("expected only the doc with a vector, got %+v", got)
	}
}

func TestVectorDescribe(t *testing.T) {
	s := NewVectorStrategyWithStore(fakeVectorStore{}, "qwen3")

	got := s.Describe()
	if got.Strategy != string(StrategyVector) {
		t.Errorf("Strategy = %q, want %q", got.Strategy, StrategyVector)
	}
	if got.Model != "qwen3" {
		t.Errorf("Model = %q, want %q", got.Model, "qwen3")
	}
}

func TestCosine(t *testing.T) {
	cases := []struct {
		name string
		a, b []float32
		want float64
	}{
		{"identical", []float32{1, 0}, []float32{1, 0}, 1},
		{"orthogonal", []float32{1, 0}, []float32{0, 1}, 0},
		{"opposite", []float32{1, 0}, []float32{-1, 0}, -1},
		{"len mismatch", []float32{1, 0}, []float32{1}, 0},
		{"empty", nil, []float32{1}, 0},
		{"zero vector", []float32{0, 0}, []float32{1, 0}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cosine(c.a, c.b); got != c.want {
				t.Errorf("cosine = %v, want %v", got, c.want)
			}
		})
	}
}
