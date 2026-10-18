package metrics

import "github.com/google/uuid"

// BinaryPreference measures ranking quality using only judged documents.
// Unlike AP/NDCG, it ignores unjudged documents entirely, making it robust
// when judgment sets are incomplete (typical in pooling workflows).
//
// For each judged relevant document in the ranking, its contribution is:
//
//	1 - min(nonRelevantAbove, R) / min(R, N)
//
// where R and N count the judged relevant and judged non-relevant documents,
// as in trec_eval. With N = 0 every retrieved relevant document scores 1.
// Non-relevant here means explicitly judged as non-relevant (in judgments, rel < threshold).
// Unjudged documents (absent from judgments) are skipped.
func BinaryPreference(ranked []uuid.UUID, judgments map[uuid.UUID]int, relevanceThreshold int) float64 {
	if len(judgments) == 0 {
		return 0
	}

	R := countRelevant(judgments, relevanceThreshold)
	if R == 0 {
		return 0
	}
	N := len(judgments) - R

	var sum float64
	var nonRelAbove int

	for _, docID := range ranked {
		rel, judged := judgments[docID]
		if !judged {
			continue
		}
		if rel >= relevanceThreshold {
			if N > 0 {
				sum += 1 - float64(min(nonRelAbove, R))/float64(min(R, N))
			} else {
				sum++
			}
		} else {
			nonRelAbove++
		}
	}

	return sum / float64(R)
}
