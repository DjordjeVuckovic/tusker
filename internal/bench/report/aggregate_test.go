package report

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/bench/metrics"
	"github.com/DjordjeVuckovic/tusker/internal/bench/runner"
	"github.com/google/uuid"
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

func TestGenerate_JudgedCoverageIsTheGradedShareOfRetrievedTopK(t *testing.T) {
	graded := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	ungraded := []uuid.UUID{uuid.New(), uuid.New()}
	grades := map[string]int{graded[0].String(): 2, graded[1].String(): 0, graded[2].String(): 1}

	retrieved := func(qid, engine string, ranked ...uuid.UUID) runner.QueryResult {
		qr := answered(qid, engine, 0.5, time.Millisecond)
		qr.RankedDocIDs = ranked
		return qr
	}
	br := &runner.BenchmarkResult{
		Config: runner.Config{
			KValues:            []int{2, 3},
			RelevanceThreshold: 1,
			Judgments:          map[string]map[string]int{"q1": grades, "q2": grades},
		},
		Jobs: []*runner.JobResult{{
			JobName:     "coverage",
			EngineNames: []string{"pg", "es"},
			QueryOrder:  []string{"q1", "q2"},
			Results: map[string]map[string]runner.QueryResult{
				"q1": {
					"pg": retrieved("q1", "pg", graded[0], ungraded[0], graded[1], graded[2]),
					"es": retrieved("q1", "es", graded[0]),
				},
				"q2": {
					"pg": retrieved("q2", "pg", ungraded[1]),
					"es": failed("q2", "es"),
				},
			},
		}},
	}

	agg := aggregatedByEngine(Generate(br, nil).Jobs[0].Aggregated)

	// Top 3: pg retrieves g0 u0 g1 and u1, so 2 of 4 carry a grade; es
	// retrieves g0 and nothing for its failed query.
	require.NotNil(t, agg["pg"].JudgedCoverage)
	require.NotNil(t, agg["es"].JudgedCoverage)
	assert.InDelta(t, 0.5, *agg["pg"].JudgedCoverage, 1e-9)
	assert.InDelta(t, 1.0, *agg["es"].JudgedCoverage, 1e-9)
}

func TestWriteTable_ShowsJudgedCoverage(t *testing.T) {
	r := &Report{
		Config: ReportConfig{KValues: []int{10}, RelevanceThreshold: 1},
		Jobs: []JobReport{{
			JobName:    "coverage",
			PerQuery:   []Entry{{QueryID: "q1", EngineName: "pg", Judged: true}},
			Aggregated: []AggregatedEntry{{EngineName: "pg", JudgedCount: 1, QueryCount: 1, JudgedCoverage: ptr(0.625)}},
		}},
	}
	var out bytes.Buffer
	WriteTable(r, &out)

	assert.Contains(t, strings.ToLower(out.String()), "coverage@10")
	assert.Contains(t, out.String(), "0.6250")
}

func TestGenerate_SignificanceIsHolmAdjustedAcrossEnginePairs(t *testing.T) {
	apByEngine := map[string][]float64{
		"a": {0.9, 0.8, 0.75, 0.6, 0.55, 0.4, 0.35},
		"b": {0.1, 0.85, 0.5, 0.2, 0.4, 0.37, 0.0},
		"c": {0.06, 0.03, 0.21, 0.1, 0.47, 0.02, 0.33},
	}
	judged := map[string]int{"11111111-1111-1111-1111-111111111111": 1}
	jr := &runner.JobResult{
		JobName:     "holm",
		EngineNames: []string{"a", "b", "c"},
		Results:     make(map[string]map[string]runner.QueryResult),
	}
	br := &runner.BenchmarkResult{
		Config: runner.Config{KValues: []int{10}, RelevanceThreshold: 1, Judgments: map[string]map[string]int{}},
		Jobs:   []*runner.JobResult{jr},
	}
	for i := range apByEngine["a"] {
		qid := fmt.Sprintf("q%d", i)
		br.Config.Judgments[qid] = judged
		jr.QueryOrder = append(jr.QueryOrder, qid)
		jr.Results[qid] = make(map[string]runner.QueryResult)
		for engine, aps := range apByEngine {
			jr.Results[qid][engine] = answered(qid, engine, aps[i], time.Millisecond)
		}
	}

	byPair := make(map[string]PairwiseSignificance)
	for _, s := range Generate(br, nil).Jobs[0].Significance {
		if s.Metric == "MAP" {
			byPair[s.EngineA+"-"+s.EngineB] = s
		}
	}

	// Raw p from scipy.stats.wilcoxon (exact); adjusted from statsmodels
	// multipletests(method="holm") over the three pairs.
	tests := []struct {
		pair          string
		raw, adjusted float64
		stars         string
	}{
		{pair: "a-b", raw: 0.046875, adjusted: 0.09375, stars: ""},
		{pair: "a-c", raw: 0.015625, adjusted: 0.046875, stars: "*"},
		{pair: "b-c", raw: 0.296875, adjusted: 0.296875, stars: ""},
	}
	for _, tc := range tests {
		t.Run(tc.pair, func(t *testing.T) {
			got, ok := byPair[tc.pair]
			require.True(t, ok)
			assert.InDelta(t, tc.raw, got.P, 1e-12)
			require.NotNil(t, got.AdjustedP)
			assert.InDelta(t, tc.adjusted, *got.AdjustedP, 1e-12)
			assert.Equal(t, tc.stars, got.Stars)
		})
	}
}

func TestWriters_ShowRawAndHolmAdjustedP(t *testing.T) {
	r := &Report{
		Config: ReportConfig{KValues: []int{10}, RelevanceThreshold: 1},
		Jobs: []JobReport{{
			JobName:      "holm",
			PerQuery:     []Entry{{QueryID: "q1", EngineName: "a", Judged: true}},
			Aggregated:   []AggregatedEntry{{EngineName: "a", JudgedCount: 1, QueryCount: 1}},
			Significance: []PairwiseSignificance{{EngineA: "a", EngineB: "b", Metric: "MAP", W: 2, P: 0.0123, AdjustedP: ptr(0.0369), Stars: "*"}},
		}},
	}
	var terminal, markdown bytes.Buffer
	WriteTable(r, &terminal)
	WriteMarkdown(r, &markdown)

	for name, out := range map[string]string{"table": terminal.String(), "markdown": markdown.String()} {
		assert.Contains(t, out, "0.0123", name)
		assert.Contains(t, out, "0.0369", name)
	}
}

func TestWriters_NameTheNDCGGainInEveryNDCGHeader(t *testing.T) {
	r := &Report{
		Config: ReportConfig{KValues: []int{5, 10}, RelevanceThreshold: 1},
		Jobs: []JobReport{{
			JobName:    "gain",
			PerQuery:   []Entry{{QueryID: "q1", EngineName: "a", Judged: true, NDCG: map[int]float64{10: 0.5}}},
			Aggregated: []AggregatedEntry{{EngineName: "a", JudgedCount: 1, QueryCount: 1, NDCG: map[int]float64{5: 0.4, 10: 0.5}}},
		}},
	}
	var terminal, markdown bytes.Buffer
	WriteTable(r, &terminal)
	WriteMarkdown(r, &markdown)
	htmlPath := filepath.Join(t.TempDir(), "report.html")
	require.NoError(t, WriteHTML(r, htmlPath))
	html, err := os.ReadFile(htmlPath)
	require.NoError(t, err)

	headers := map[string][]string{
		"table":    regexp.MustCompile(`(?i)NDCG@\d+[^│]*`).FindAllString(terminal.String(), -1),
		"markdown": regexp.MustCompile(`NDCG@\d+[^|]*`).FindAllString(markdown.String(), -1),
		"html":     regexp.MustCompile(`<(?:th|div class="section-title")>NDCG@[^<]*<`).FindAllString(string(html), -1),
	}
	for writer, found := range headers {
		require.NotEmpty(t, found, writer)
		for _, header := range found {
			assert.Contains(t, strings.ToLower(header), "2^rel-1", "%s header %q", writer, header)
		}
	}
}

func TestWriters_ShowUnrecordedScoringFieldsAsAbsentNotZero(t *testing.T) {
	r := &Report{
		Config: ReportConfig{KValues: []int{10}, RelevanceThreshold: 1},
		Jobs: []JobReport{{
			JobName:      "legacy",
			PerQuery:     []Entry{{QueryID: "q1", EngineName: "a", Judged: true}},
			Aggregated:   []AggregatedEntry{{EngineName: "a", JudgedCount: 1, QueryCount: 1, MAP: 0.5, MRR: 0.6, MBpref: 0.7}},
			Significance: []PairwiseSignificance{{EngineA: "a", EngineB: "b", Metric: "MAP", W: 2, P: 0.0123, Stars: "*"}},
		}},
	}
	var terminal, markdown bytes.Buffer
	WriteTable(r, &terminal)
	WriteMarkdown(r, &markdown)

	assert.Regexp(t, `0\.7000\s+│\s+—\s+│`, terminal.String(), "coverage cell")
	assert.Regexp(t, `0\.0123\s+│\s+—\s+│`, terminal.String(), "holm cell")
	assert.Contains(t, markdown.String(), "| 0.0123 | — |", "holm cell")
}

func TestWriteTable_JudgedJobWhereEveryCellFailedIsNotLatencyOnly(t *testing.T) {
	r := Generate(unevenProbe(), nil)
	for i := range r.Jobs[0].PerQuery {
		r.Jobs[0].PerQuery[i].Judged = false
		r.Jobs[0].PerQuery[i].Error = "timeout"
	}

	var out bytes.Buffer
	WriteTable(r, &out)

	assert.NotContains(t, out.String(), "No relevance judgments found")
}

func ptr(v float64) *float64 { return &v }

func TestWriters_LatencyTablesShowExcludedQueries(t *testing.T) {
	r := &Report{
		Config: ReportConfig{KValues: []int{10}, RelevanceThreshold: 1},
		Jobs: []JobReport{{
			JobName:    "excluded",
			PerQuery:   []Entry{{QueryID: "q1", EngineName: "a", Judged: true}},
			Aggregated: []AggregatedEntry{{EngineName: "a", JudgedCount: 7, QueryCount: 7, LatencyExcludedQueries: 7}},
		}},
	}
	var terminal, markdown bytes.Buffer
	WriteTable(r, &terminal)
	WriteMarkdown(r, &markdown)
	htmlPath := filepath.Join(t.TempDir(), "report.html")
	require.NoError(t, WriteHTML(r, htmlPath))
	html, err := os.ReadFile(htmlPath)
	require.NoError(t, err)

	assert.Regexp(t, `(?i)samples\s+│\s+excluded`, terminal.String())
	assert.Regexp(t, `│\s+0\s+│\s+7\s+│`, terminal.String())
	assert.Contains(t, markdown.String(), "| Samples | Excluded |")
	assert.Contains(t, markdown.String(), "| 0 | 7 |")
	assert.Contains(t, string(html), "<th>Excluded</th>")
	assert.Contains(t, string(html), "<td>7</td>")
}

func TestWriters_MarkdownAndHTMLShowJudgedCoverage(t *testing.T) {
	r := &Report{
		Config: ReportConfig{KValues: []int{10}, RelevanceThreshold: 1},
		Jobs: []JobReport{{
			JobName:    "coverage",
			PerQuery:   []Entry{{QueryID: "q1", EngineName: "pg", Judged: true}},
			Aggregated: []AggregatedEntry{{EngineName: "pg", JudgedCount: 1, QueryCount: 1, JudgedCoverage: ptr(0.625)}},
		}},
	}
	var markdown bytes.Buffer
	WriteMarkdown(r, &markdown)
	htmlPath := filepath.Join(t.TempDir(), "report.html")
	require.NoError(t, WriteHTML(r, htmlPath))
	html, err := os.ReadFile(htmlPath)
	require.NoError(t, err)

	assert.Contains(t, markdown.String(), "Coverage@10")
	assert.Contains(t, markdown.String(), "| 0.6250 |")
	assert.Contains(t, string(html), "<th>Coverage@10</th>")
	assert.Contains(t, string(html), "0.6250")
}

func TestGenerate_JudgedCoverageIsUnknownForSuiteEmbeddedGrades(t *testing.T) {
	qr := answered("q1", "pg", 0.5, time.Millisecond)
	qr.RankedDocIDs = []uuid.UUID{uuid.New(), uuid.New()}
	br := &runner.BenchmarkResult{
		Config: runner.Config{KValues: []int{10}, RelevanceThreshold: 1},
		Jobs: []*runner.JobResult{{
			JobName:     "suite-graded",
			EngineNames: []string{"pg"},
			QueryOrder:  []string{"q1"},
			Results:     map[string]map[string]runner.QueryResult{"q1": {"pg": qr}},
		}},
	}

	agg := Generate(br, nil).Jobs[0].Aggregated[0]

	require.Equal(t, 1, agg.JudgedCount)
	assert.Nil(t, agg.JudgedCoverage, "the report never sees suite-embedded grades, so coverage is unknown, not 0")
}

func TestGenerate_LatencyExcludesAQueryAnEngineHadNoBlockFor(t *testing.T) {
	judged := map[string]int{"11111111-1111-1111-1111-111111111111": 1}
	br := &runner.BenchmarkResult{
		Config: runner.Config{KValues: []int{10}, RelevanceThreshold: 1, Judgments: map[string]map[string]int{"q1": judged, "q2": judged}},
		Jobs: []*runner.JobResult{{
			JobName:     "missing-block",
			EngineNames: []string{"pg", "es"},
			QueryOrder:  []string{"q1", "q2"},
			Results: map[string]map[string]runner.QueryResult{
				"q1": {"pg": answered("q1", "pg", 0.5, 10*time.Millisecond), "es": answered("q1", "es", 0.5, 10*time.Millisecond)},
				"q2": {"pg": answered("q2", "pg", 0.5, time.Second)},
			},
		}},
	}

	agg := aggregatedByEngine(Generate(br, nil).Jobs[0].Aggregated)

	assert.Less(t, agg["pg"].Latency.Max, time.Second, "es had no block for q2, so pg's q2 latency stays out")
	assert.Equal(t, 1, agg["pg"].LatencyExcludedQueries)
}
