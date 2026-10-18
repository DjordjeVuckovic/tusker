package metrics

import (
	"math"
	"sort"
)

// PairwiseResult is the outcome of a two-tailed Wilcoxon signed-rank test
// comparing per-query scores of two engines on one metric.
type PairwiseResult struct {
	W float64 // test statistic (smaller of W+ and W-)
	P float64 // two-tailed p-value, uncorrected for multiple comparisons
}

// differencePrecision is the grid differences are rounded to before the zero
// test and ranking, so equal differences that land on different floats
// (1/3-1/6 and 1/2-1/3) tie.
const differencePrecision = 1e12

// maxExactPairs is the largest untied sample whose p-value comes from the
// exact null distribution, the cutoff scipy.stats.wilcoxon uses.
const maxExactPairs = 50

// Wilcoxon performs the two-tailed Wilcoxon signed-rank test on paired scores.
// a[i] and b[i] are per-query scores for the same query under two engines.
// Zero differences are dropped. The p-value is exact for up to 50 untied
// pairs, otherwise the normal approximation with tie and continuity
// correction. Returns nil when fewer than 4 non-zero differences remain.
func Wilcoxon(a, b []float64) *PairwiseResult {
	if len(a) != len(b) || len(a) == 0 {
		return nil
	}

	type observation struct {
		absD float64
		sign float64
	}

	var obs []observation
	for i := range a {
		d := math.Round((a[i]-b[i])*differencePrecision) / differencePrecision
		if d == 0 {
			continue
		}
		s := 1.0
		if d < 0 {
			s = -1.0
		}
		obs = append(obs, observation{math.Abs(d), s})
	}
	n := len(obs)
	if n < 4 {
		return nil
	}

	sort.Slice(obs, func(i, j int) bool { return obs[i].absD < obs[j].absD })

	ranks := make([]float64, n)
	tieCorr := 0.0
	for i := 0; i < n; {
		j := i
		for j < n && obs[j].absD == obs[i].absD {
			j++
		}
		avg := float64(i+1+j) / 2.0 // 1-indexed average
		t := float64(j - i)
		if t > 1 {
			tieCorr += t*t*t - t
		}
		for k := i; k < j; k++ {
			ranks[k] = avg
		}
		i = j
	}

	wPlus, wMinus := 0.0, 0.0
	for i, o := range obs {
		if o.sign > 0 {
			wPlus += ranks[i]
		} else {
			wMinus += ranks[i]
		}
	}
	W := math.Min(wPlus, wMinus)

	if n <= maxExactPairs && tieCorr == 0 {
		return &PairwiseResult{W: W, P: exactSignedRankP(n, int(W))}
	}

	fn := float64(n)
	mu := fn * (fn + 1) / 4
	variance := fn*(fn+1)*(2*fn+1)/24 - tieCorr/48
	if variance <= 0 {
		return nil
	}
	// Continuity correction moves W half a rank toward the mean, as scipy's
	// correction=True does.
	shifted := W - mu
	if shifted != 0 {
		shifted += 0.5
	}
	z := shifted / math.Sqrt(variance)
	return &PairwiseResult{W: W, P: 2 * normCDF(-math.Abs(z))}
}

// exactSignedRankP is the two-tailed p-value of statistic w under the null
// distribution of the signed-rank sum over untied ranks 1..n.
func exactSignedRankP(n, w int) float64 {
	maxSum := n * (n + 1) / 2
	subsetsWithSum := make([]float64, maxSum+1)
	subsetsWithSum[0] = 1
	for rank := 1; rank <= n; rank++ {
		for s := maxSum; s >= rank; s-- {
			subsetsWithSum[s] += subsetsWithSum[s-rank]
		}
	}
	var atMostW float64
	for s := 0; s <= w; s++ {
		atMostW += subsetsWithSum[s]
	}
	return math.Min(1, 2*atMostW/math.Pow(2, float64(n)))
}

// Holm returns the Holm-Bonferroni adjusted p-values of one family of tests,
// in the order given.
func Holm(pValues []float64) []float64 {
	m := len(pValues)
	order := make([]int, m)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return pValues[order[i]] < pValues[order[j]] })

	adjusted := make([]float64, m)
	running := 0.0
	for step, idx := range order {
		running = math.Max(running, math.Min(1, float64(m-step)*pValues[idx]))
		adjusted[idx] = running
	}
	return adjusted
}

// SignificanceStars marks p: "**" below 0.01, "*" below 0.05, "" otherwise.
func SignificanceStars(p float64) string {
	switch {
	case p < 0.01:
		return "**"
	case p < 0.05:
		return "*"
	}
	return ""
}

// normCDF returns P(Z ≤ x) for the standard normal distribution.
func normCDF(x float64) float64 {
	return 0.5 * math.Erfc(-x/math.Sqrt2)
}
