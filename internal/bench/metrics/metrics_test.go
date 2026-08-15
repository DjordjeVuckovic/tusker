package metrics

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func newIDs(n int) []uuid.UUID {
	ids := make([]uuid.UUID, n)
	for i := range ids {
		ids[i] = uuid.New()
	}
	return ids
}

func TestNDCGAtK(t *testing.T) {
	ids := newIDs(5)

	tests := []struct {
		name      string
		ranked    []uuid.UUID
		judgments map[uuid.UUID]int
		k         int
		threshold int
		want      float64
	}{
		{
			name:      "empty ranked list",
			ranked:    nil,
			judgments: map[uuid.UUID]int{ids[0]: 3},
			k:         5,
			want:      0,
		},
		{
			name:      "empty judgments",
			ranked:    ids[:3],
			judgments: map[uuid.UUID]int{},
			k:         5,
			want:      0,
		},
		{
			name:      "k=0",
			ranked:    ids[:3],
			judgments: map[uuid.UUID]int{ids[0]: 3},
			k:         0,
			want:      0,
		},
		{
			name:   "perfect ranking",
			ranked: ids[:3],
			judgments: map[uuid.UUID]int{
				ids[0]: 3,
				ids[1]: 2,
				ids[2]: 1,
			},
			k:    3,
			want: 1.0,
		},
		{
			name:   "inverse ranking",
			ranked: []uuid.UUID{ids[2], ids[1], ids[0]},
			judgments: map[uuid.UUID]int{
				ids[0]: 3,
				ids[1]: 2,
				ids[2]: 1,
			},
			k:    3,
			want: 0.0, // will be < 1
		},
		{
			name:   "single highly relevant at top",
			ranked: ids[:3],
			judgments: map[uuid.UUID]int{
				ids[0]: 3,
			},
			k:    3,
			want: 1.0,
		},
		{
			name:   "threshold 2 ignores marginal grades",
			ranked: []uuid.UUID{ids[0], ids[3], ids[4]},
			judgments: map[uuid.UUID]int{
				ids[0]: 3,
				ids[1]: 1,
				ids[2]: 1,
			},
			k:         3,
			threshold: 2,
			want:      1.0,
		},
		{
			name:   "threshold 2 gives a misplaced marginal doc no gain",
			ranked: []uuid.UUID{ids[1], ids[0]},
			judgments: map[uuid.UUID]int{
				ids[0]: 3,
				ids[1]: 1,
			},
			k:         10,
			threshold: 2,
			want:      0.6309297535714575,
		},
		{
			name:   "threshold 1 credits the same misplaced marginal doc",
			ranked: []uuid.UUID{ids[1], ids[0]},
			judgments: map[uuid.UUID]int{
				ids[0]: 3,
				ids[1]: 1,
			},
			k:         10,
			threshold: 1,
			want:      0.7098097413968655,
		},
		{
			name:   "threshold 1 counts marginal grades in the ideal",
			ranked: []uuid.UUID{ids[0], ids[3], ids[4]},
			judgments: map[uuid.UUID]int{
				ids[0]: 3,
				ids[1]: 1,
				ids[2]: 1,
			},
			k:         3,
			threshold: 1,
			// DCG 7 over IDCG 7 + 1/log2(3) + 1/log2(4).
			want: 0.8609101556836468,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NDCGAtK(tt.ranked, tt.judgments, tt.k, maxInt(tt.threshold, 1))
			if tt.name == "inverse ranking" {
				assert.Less(t, got, 1.0)
				assert.Greater(t, got, 0.0)
			} else {
				assert.InDelta(t, tt.want, got, 1e-9)
			}
		})
	}
}

func TestPrecisionAtK(t *testing.T) {
	ids := newIDs(5)

	tests := []struct {
		name      string
		ranked    []uuid.UUID
		judgments map[uuid.UUID]int
		k         int
		threshold int
		want      float64
	}{
		{
			name:      "empty",
			ranked:    nil,
			judgments: map[uuid.UUID]int{},
			k:         5,
			threshold: 1,
			want:      0,
		},
		{
			name:   "all relevant",
			ranked: ids[:3],
			judgments: map[uuid.UUID]int{
				ids[0]: 2,
				ids[1]: 1,
				ids[2]: 3,
			},
			k:         3,
			threshold: 1,
			want:      1.0,
		},
		{
			name:   "half relevant",
			ranked: ids[:4],
			judgments: map[uuid.UUID]int{
				ids[0]: 2,
				ids[2]: 1,
			},
			k:         4,
			threshold: 1,
			want:      0.5,
		},
		{
			name:   "none relevant",
			ranked: ids[:3],
			judgments: map[uuid.UUID]int{
				ids[0]: 0,
			},
			k:         3,
			threshold: 1,
			want:      0,
		},
		{
			name:   "k larger than ranked list",
			ranked: ids[:2],
			judgments: map[uuid.UUID]int{
				ids[0]: 2,
				ids[1]: 2,
			},
			k:         5,
			threshold: 1,
			want:      0.4, // 2/5
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PrecisionAtK(tt.ranked, tt.judgments, tt.k, tt.threshold)
			assert.InDelta(t, tt.want, got, 1e-9)
		})
	}
}

func TestRecallAtK(t *testing.T) {
	ids := newIDs(5)

	tests := []struct {
		name      string
		ranked    []uuid.UUID
		judgments map[uuid.UUID]int
		k         int
		threshold int
		want      float64
	}{
		{
			name:      "no relevant in judgments",
			ranked:    ids[:3],
			judgments: map[uuid.UUID]int{ids[0]: 0},
			k:         3,
			threshold: 1,
			want:      0,
		},
		{
			name:   "all found in top-K",
			ranked: ids[:3],
			judgments: map[uuid.UUID]int{
				ids[0]: 2,
				ids[1]: 1,
			},
			k:         3,
			threshold: 1,
			want:      1.0,
		},
		{
			name:   "partial recall",
			ranked: ids[:2],
			judgments: map[uuid.UUID]int{
				ids[0]: 2,
				ids[1]: 1,
				ids[3]: 3, // not in top-K
			},
			k:         2,
			threshold: 1,
			want:      2.0 / 3.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RecallAtK(tt.ranked, tt.judgments, tt.k, tt.threshold)
			assert.InDelta(t, tt.want, got, 1e-9)
		})
	}
}

func TestF1AtK(t *testing.T) {
	ids := newIDs(4)

	judgments := map[uuid.UUID]int{
		ids[0]: 2,
		ids[1]: 1,
		ids[2]: 0,
		ids[3]: 3,
	}
	ranked := []uuid.UUID{ids[0], ids[2], ids[1]}

	// P@3 = 2/3, R@3 = 2/3 (3 relevant: ids[0], ids[1], ids[3] — 2 found)
	f1 := F1AtK(ranked, judgments, 3, 1)
	assert.InDelta(t, 2.0/3.0, f1, 1e-9)
}

func TestAveragePrecision(t *testing.T) {
	ids := newIDs(5)

	tests := []struct {
		name      string
		ranked    []uuid.UUID
		judgments map[uuid.UUID]int
		threshold int
		want      float64
	}{
		{
			name:      "empty",
			ranked:    nil,
			judgments: map[uuid.UUID]int{},
			threshold: 1,
			want:      0,
		},
		{
			name:   "perfect ranking",
			ranked: ids[:3],
			judgments: map[uuid.UUID]int{
				ids[0]: 2,
				ids[1]: 1,
				ids[2]: 1,
			},
			threshold: 1,
			// Precision at each relevant rank: 1/1, 2/2, 3/3 = 1.0
			want: 1.0,
		},
		{
			name:   "relevant at positions 1 and 3",
			ranked: []uuid.UUID{ids[0], ids[2], ids[1]},
			judgments: map[uuid.UUID]int{
				ids[0]: 2,
				ids[1]: 1,
			},
			threshold: 1,
			// Precision at relevant positions: 1/1=1.0, 2/3=0.667
			// AP = (1.0 + 0.667) / 2 = 0.833
			want: (1.0 + 2.0/3.0) / 2.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AveragePrecision(tt.ranked, tt.judgments, tt.threshold)
			assert.InDelta(t, tt.want, got, 1e-9)
		})
	}
}

func TestReciprocalRank(t *testing.T) {
	ids := newIDs(5)

	tests := []struct {
		name      string
		ranked    []uuid.UUID
		judgments map[uuid.UUID]int
		threshold int
		want      float64
	}{
		{
			name:      "no relevant docs",
			ranked:    ids[:3],
			judgments: map[uuid.UUID]int{},
			threshold: 1,
			want:      0,
		},
		{
			name:   "first is relevant",
			ranked: ids[:3],
			judgments: map[uuid.UUID]int{
				ids[0]: 2,
			},
			threshold: 1,
			want:      1.0,
		},
		{
			name:   "third is first relevant",
			ranked: ids[:5],
			judgments: map[uuid.UUID]int{
				ids[2]: 1,
				ids[4]: 2,
			},
			threshold: 1,
			want:      1.0 / 3.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReciprocalRank(tt.ranked, tt.judgments, tt.threshold)
			assert.InDelta(t, tt.want, got, 1e-9)
		})
	}
}

func TestComputeAll(t *testing.T) {
	ids := newIDs(3)
	judgments := map[uuid.UUID]int{
		ids[0]: 3,
		ids[1]: 2,
		ids[2]: 1,
	}
	ranked := []uuid.UUID{ids[0], ids[1], ids[2]}

	scores := ComputeAll(ranked, judgments, []int{3, 5, 10}, 1)

	assert.InDelta(t, 1.0, scores.NDCG[3], 1e-9)
	assert.InDelta(t, 1.0, scores.Precision[3], 1e-9)
	assert.InDelta(t, 1.0, scores.Recall[3], 1e-9)
	assert.InDelta(t, 1.0, scores.F1[3], 1e-9)
	assert.InDelta(t, 1.0, scores.AP, 1e-9)
	assert.InDelta(t, 1.0, scores.RR, 1e-9)

	assert.Contains(t, scores.NDCG, 5)
	assert.Contains(t, scores.NDCG, 10)
}

// Cases that set no threshold run at the default of 1.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
