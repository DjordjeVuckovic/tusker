package report

import (
	"errors"
	"testing"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/bench/metrics"
	"github.com/DjordjeVuckovic/tusker/internal/bench/runner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func answered(qid, engine string, ap float64, latency time.Duration) runner.QueryResult {
	return runner.QueryResult{
		QueryID:    qid,
		EngineName: engine,
		Scores: metrics.ScoreSet{
			Judged:    true,
			NDCG:      map[int]float64{10: ap},
			Precision: map[int]float64{10: ap},
			Recall:    map[int]float64{10: ap},
			F1:        map[int]float64{10: ap},
			AP:        ap,
			RR:        ap,
			Bpref:     ap,
		},
		Latency: runner.ComputeLatencyStats([]time.Duration{latency}),
	}
}

func failed(qid, engine string) runner.QueryResult {
	return runner.QueryResult{QueryID: qid, EngineName: engine, Error: errors.New("timeout")}
}

// pg answers q1 and q2 and fails q3 and q4; es answers q1 to q3 and has no
// block for q4. Every query is judged.
func unevenProbe() *runner.BenchmarkResult {
	judged := map[string]int{"11111111-1111-1111-1111-111111111111": 1}
	return &runner.BenchmarkResult{
		Config: runner.Config{
			KValues:            []int{10},
			RelevanceThreshold: 1,
			Judgments:          map[string]map[string]int{"q1": judged, "q2": judged, "q3": judged, "q4": judged},
		},
		Jobs: []*runner.JobResult{{
			JobName:     "probe",
			EngineNames: []string{"pg", "es"},
			QueryOrder:  []string{"q1", "q2", "q3", "q4"},
			Results: map[string]map[string]runner.QueryResult{
				"q1": {"pg": answered("q1", "pg", 0.8, 10*time.Millisecond), "es": answered("q1", "es", 0.7, 10*time.Millisecond)},
				"q2": {"pg": answered("q2", "pg", 0.8, 10*time.Millisecond), "es": answered("q2", "es", 0.7, 10*time.Millisecond)},
				"q3": {"pg": failed("q3", "pg"), "es": answered("q3", "es", 0.1, time.Second)},
				"q4": {"pg": failed("q4", "pg")},
			},
		}},
	}
}

func aggregatedByEngine(entries []AggregatedEntry) map[string]AggregatedEntry {
	byEngine := make(map[string]AggregatedEntry, len(entries))
	for _, e := range entries {
		byEngine[e.EngineName] = e
	}
	return byEngine
}

func TestGenerate_ScoresEveryEngineOverTheSameJudgedQueries(t *testing.T) {
	agg := aggregatedByEngine(Generate(unevenProbe(), nil).Jobs[0].Aggregated)

	// trec_eval -c over the four judged topics.
	assert.InDelta(t, 0.400, agg["pg"].MAP, 1e-9)
	assert.InDelta(t, 0.375, agg["es"].MAP, 1e-9)
	assert.InDelta(t, 0.400, agg["pg"].NDCG[10], 1e-9)
	assert.InDelta(t, 0.375, agg["es"].MRR, 1e-9)
	assert.InDelta(t, 0.375, agg["es"].MBpref, 1e-9)
	assert.InDelta(t, 0.375, agg["es"].Precision[10], 1e-9)
	assert.Equal(t, agg["pg"].JudgedCount, agg["es"].JudgedCount)
	assert.Equal(t, 1, agg["es"].ErrorCount, "the missing q4 block counts as an error")
}

func TestGenerate_NDCGStddevIncludesFailedQueriesAsZero(t *testing.T) {
	agg := aggregatedByEngine(Generate(unevenProbe(), nil).Jobs[0].Aggregated)

	// Sample stddev of {0.8, 0.8, 0, 0}: sqrt(4 * 0.4^2 / 3).
	assert.InDelta(t, 0.46188021535170065, agg["pg"].NDCGStddev[10], 1e-9)
}

func TestGenerate_LatencyCoversOnlyQueriesEveryEngineAnswered(t *testing.T) {
	agg := aggregatedByEngine(Generate(unevenProbe(), nil).Jobs[0].Aggregated)

	require.NotZero(t, agg["es"].Latency.Max)
	assert.Less(t, agg["es"].Latency.Max, time.Second, "es answered q3 alone, so its latency stays out")
	assert.Equal(t, 2, agg["es"].LatencyExcludedQueries)
	assert.Equal(t, 2, agg["pg"].LatencyExcludedQueries)
}

func TestGenerate_SignificancePairsFailedQueriesAsZero(t *testing.T) {
	judged := map[string]int{"11111111-1111-1111-1111-111111111111": 1}
	jr := &runner.JobResult{
		JobName:     "pairs",
		EngineNames: []string{"pg", "es"},
		Results:     make(map[string]map[string]runner.QueryResult),
	}
	br := &runner.BenchmarkResult{
		Config: runner.Config{KValues: []int{10}, RelevanceThreshold: 1, Judgments: map[string]map[string]int{}},
		Jobs:   []*runner.JobResult{jr},
	}
	for i, qid := range []string{"q1", "q2", "q3", "q4", "q5", "q6"} {
		br.Config.Judgments[qid] = judged
		jr.QueryOrder = append(jr.QueryOrder, qid)
		if i == 0 {
			jr.Results[qid] = map[string]runner.QueryResult{"pg": failed(qid, "pg"), "es": answered(qid, "es", 0.5, time.Millisecond)}
			continue
		}
		jr.Results[qid] = map[string]runner.QueryResult{
			"pg": answered(qid, "pg", 0.75, time.Millisecond),
			"es": answered(qid, "es", 0.25, time.Millisecond),
		}
	}

	var mapTest *PairwiseSignificance
	for _, s := range Generate(br, nil).Jobs[0].Significance {
		if s.Metric == "MAP" {
			mapTest = &s
		}
	}

	// Six differences of |0.5|, one of them negative: each ranks 3.5, so W- = 3.5.
	require.NotNil(t, mapTest)
	assert.InDelta(t, 3.5, mapTest.W, 1e-9)
}
