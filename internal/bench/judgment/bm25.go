package judgment

import (
	"context"
	"math"
	"sort"
	"strings"

	"github.com/DjordjeVuckovic/tusker/internal/bench/meta"
)

const (
	bm25K1 = 1.5
	bm25B  = 0.75

	// poolBatchSize makes the runner hand a query's whole candidate pool to
	// GradeBatch in one call, so BM25 statistics (idf, avgdl) are computed over
	// the full pool rather than an arbitrary chunk of it. Per-query pools are
	// pool_depth × engines deduped (≈50-150), comfortably under this.
	poolBatchSize = 512
)

// BM25Strategy grades candidates with Okapi BM25 computed over the per-query
// candidate pool as a local corpus: idf from pool document frequencies, avgdl
// from pool documents. Each doc's score is normalised against the top-scoring
// candidate and mapped to a grade 0-3.
//
// It is a heuristic, no-network judge — stronger than lexical token-overlap
// because it rewards rarer terms and saturates term frequency, but still purely
// keyword-based with no semantic understanding. Use it as a reproducible
// baseline alongside the LLM judges.
type BM25Strategy struct{}

func NewBM25Strategy() *BM25Strategy { return &BM25Strategy{} }

func (BM25Strategy) Name() string { return string(StrategyBM25) }

func (BM25Strategy) Describe() meta.Judge {
	return meta.Judge{Strategy: string(StrategyBM25)}
}

func (BM25Strategy) PreferredBatchSize() int { return poolBatchSize }

func (s BM25Strategy) GradeBatch(_ context.Context, q GradingQuery, docs []GradingDoc) ([]GradedDoc, error) {
	norms := s.normScores(q, docs)
	out := make([]GradedDoc, len(docs))
	for i, d := range docs {
		out[i] = GradedDoc{DocID: d.ID, Grade: gradeFromNorm(norms[i])}
	}
	return out, nil
}

// Grade is a per-doc fallback (the runner only reaches it if GradeBatch fails,
// which the no-network BM25 path does not). Without the pool there is no idf or
// avgdl to compute, so it degrades to the lexical coverage baseline.
func (BM25Strategy) Grade(ctx context.Context, q GradingQuery, doc GradingDoc) (int, error) {
	return LexicalStrategy{}.Grade(ctx, q, doc)
}

// normScores returns each doc's BM25 score normalised to [0,1] against the
// top-scoring candidate in the pool. Shared with HybridStrategy.
func (BM25Strategy) normScores(q GradingQuery, docs []GradingDoc) []float64 {
	norms := make([]float64, len(docs))
	terms := tokenizeSlice(q.Description)
	if len(terms) == 0 || len(docs) == 0 {
		return norms
	}

	counts := make([]map[string]int, len(docs))
	lengths := make([]int, len(docs))
	df := map[string]int{}
	totalLen := 0
	for i, d := range docs {
		c := tokenCounts(docText(d))
		counts[i] = c
		for _, n := range c {
			lengths[i] += n
		}
		totalLen += lengths[i]
		for t := range c {
			df[t]++
		}
	}

	avgdl := float64(totalLen) / float64(len(docs))
	if avgdl == 0 {
		avgdl = 1
	}
	n := float64(len(docs))

	scores := make([]float64, len(docs))
	maxScore := 0.0
	for i := range docs {
		dl := float64(lengths[i])
		var score float64
		for _, t := range terms {
			tf := float64(counts[i][t])
			if tf == 0 {
				continue
			}
			idf := math.Log(1 + (n-float64(df[t])+0.5)/(float64(df[t])+0.5))
			score += idf * (tf * (bm25K1 + 1)) / (tf + bm25K1*(1-bm25B+bm25B*dl/avgdl))
		}
		scores[i] = score
		if score > maxScore {
			maxScore = score
		}
	}

	if maxScore > 0 {
		for i := range docs {
			norms[i] = scores[i] / maxScore
		}
	}
	return norms
}

// gradeFromNorm maps a [0,1] relevance score to a grade. Shared by BM25 and the
// hybrid fusion score.
func gradeFromNorm(norm float64) int {
	switch {
	case norm >= 0.66:
		return GradeHighly
	case norm >= 0.40:
		return GradeRelevant
	case norm >= 0.15:
		return GradeMarginally
	default:
		return GradeNotRelev
	}
}

func tokenizeSlice(s string) []string {
	set := tokenize(s)
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func tokenCounts(s string) map[string]int {
	out := map[string]int{}
	for _, raw := range tokenSplit.Split(strings.ToLower(s), -1) {
		if len(raw) < 3 {
			continue
		}
		out[raw]++
	}
	return out
}

func docText(d GradingDoc) string {
	return strings.Join([]string{d.Title, d.Description, d.Content}, " ")
}
