package metrics

import (
	"math"
	"sort"

	"github.com/google/uuid"
)

// NDCGAtK computes Normalized Discounted Cumulative Gain at rank K.
// Uses graded relevance: DCG = sum((2^rel - 1) / log2(i+2)) for i in 0..K-1.
// Grades below relevanceThreshold contribute nothing, to DCG and ideal DCG alike.
func NDCGAtK(ranked []uuid.UUID, judgments map[uuid.UUID]int, k int, relevanceThreshold int) float64 {
	if k <= 0 || len(ranked) == 0 || len(judgments) == 0 {
		return 0
	}

	dcg := dcgAtK(ranked, judgments, k, relevanceThreshold)
	idcg := idealDCGAtK(judgments, k, relevanceThreshold)

	if idcg == 0 {
		return 0
	}

	return dcg / idcg
}

func dcgAtK(ranked []uuid.UUID, judgments map[uuid.UUID]int, k int, relevanceThreshold int) float64 {
	n := min(k, len(ranked))
	var dcg float64

	for i := 0; i < n; i++ {
		rel := judgments[ranked[i]] // 0 for unjudged
		if rel < relevanceThreshold {
			continue
		}
		dcg += (math.Pow(2, float64(rel)) - 1) / math.Log2(float64(i+2))
	}

	return dcg
}

func idealDCGAtK(judgments map[uuid.UUID]int, k int, relevanceThreshold int) float64 {
	rels := make([]int, 0, len(judgments))
	for _, rel := range judgments {
		if rel >= relevanceThreshold {
			rels = append(rels, rel)
		}
	}

	sort.Sort(sort.Reverse(sort.IntSlice(rels)))

	n := min(k, len(rels))
	var idcg float64

	for i := 0; i < n; i++ {
		idcg += (math.Pow(2, float64(rels[i])) - 1) / math.Log2(float64(i+2))
	}

	return idcg
}
