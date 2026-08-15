package metrics

import "github.com/google/uuid"

const (
	GradeNotRelevant = 0
	GradeMarginally  = 1
	GradeRelevant    = 2
	GradeHighly      = 3
)

type ScoreSet struct {
	Judged    bool            // false when no relevance judgments available
	NDCG      map[int]float64 // K -> NDCG@K
	Precision map[int]float64 // K -> P@K
	Recall    map[int]float64 // K -> R@K
	F1        map[int]float64 // K -> F1@K
	AP        float64         // Average Precision
	RR        float64         // Reciprocal Rank
	Bpref     float64         // Binary Preference (robust to incomplete judgment sets)
}

func ComputeAll(ranked []uuid.UUID, judgments map[uuid.UUID]int, kValues []int, relevanceThreshold int) ScoreSet {
	s := ScoreSet{
		Judged:    true,
		NDCG:      make(map[int]float64, len(kValues)),
		Precision: make(map[int]float64, len(kValues)),
		Recall:    make(map[int]float64, len(kValues)),
		F1:        make(map[int]float64, len(kValues)),
	}

	for _, k := range kValues {
		s.NDCG[k] = NDCGAtK(ranked, judgments, k, relevanceThreshold)
		s.Precision[k] = PrecisionAtK(ranked, judgments, k, relevanceThreshold)
		s.Recall[k] = RecallAtK(ranked, judgments, k, relevanceThreshold)
		s.F1[k] = F1AtK(ranked, judgments, k, relevanceThreshold)
	}

	s.AP = AveragePrecision(ranked, judgments, relevanceThreshold)
	s.RR = ReciprocalRank(ranked, judgments, relevanceThreshold)
	s.Bpref = BinaryPreference(ranked, judgments, relevanceThreshold)

	return s
}
