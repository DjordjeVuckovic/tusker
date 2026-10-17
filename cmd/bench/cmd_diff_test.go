package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/bench/meta"
	"github.com/DjordjeVuckovic/tusker/internal/bench/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type diffRun struct {
	runID     string
	kValues   []int
	ndcg      map[int]float64
	threshold int
	judge     *meta.Judge
	judgments string
}

func diffReport(run diffRun) *report.Report {
	return &report.Report{
		SchemaVersion: 1,
		Provenance: meta.Meta{
			RunID:   run.runID,
			Judge:   run.judge,
			Sources: &meta.Sources{Judgments: run.judgments},
		},
		Config: report.ReportConfig{KValues: run.kValues, RelevanceThreshold: run.threshold},
		Jobs: []report.JobReport{{
			JobName:    "fts",
			Aggregated: []report.AggregatedEntry{{EngineName: "pg", NDCG: run.ndcg, MAP: 0.5, JudgedCount: 1, QueryCount: 1}},
			PerQuery:   []report.Entry{{QueryID: "q1", EngineName: "pg", Judged: true, NDCG: run.ndcg}},
		}},
	}
}

func TestPrintDiff_ComparesOnlyKValuesBothRunsHave(t *testing.T) {
	before := diffReport(diffRun{runID: "a", kValues: []int{3, 5, 10}, ndcg: map[int]float64{3: 0.4, 5: 0.5, 10: 0.6}, threshold: 1})
	after := diffReport(diffRun{runID: "b", kValues: []int{3, 5, 20}, ndcg: map[int]float64{3: 0.4, 5: 0.55, 20: 0.7}, threshold: 1})

	var out bytes.Buffer
	printDiff(&out, before, after)
	got := out.String()

	assert.Contains(t, got, "NDCG@5")
	assert.Contains(t, got, "0.5000→0.5500")
	assert.NotContains(t, got, "NDCG@10")
	assert.NotContains(t, got, "NDCG@20")
	assert.NotContains(t, got, "0.6000→0.0000", "a K missing from one run must not read as a zero score")
}

func TestPrintDiff_SkipsNDCGWhenNoKValueIsShared(t *testing.T) {
	before := diffReport(diffRun{runID: "a", kValues: []int{10}, ndcg: map[int]float64{10: 0.6}, threshold: 1})
	after := diffReport(diffRun{runID: "b", kValues: []int{20}, ndcg: map[int]float64{20: 0.7}, threshold: 1})

	var out bytes.Buffer
	printDiff(&out, before, after)

	assert.NotContains(t, out.String(), "-100.0%")
	assert.NotContains(t, out.String(), "Per-Query NDCG")
}

func TestPrintDiff_WarnsWhenTheRunsWereScoredDifferently(t *testing.T) {
	lexical := &meta.Judge{Strategy: "lexical"}
	llm := &meta.Judge{Strategy: "llm-cli", Provider: "claude", Model: "claude-haiku-4-5"}
	base := diffRun{runID: "a", kValues: []int{10}, ndcg: map[int]float64{10: 0.6}, threshold: 1, judge: lexical, judgments: "trec/annotations.lexical.yaml"}

	tests := []struct {
		name    string
		after   func(diffRun) diffRun
		warning string
	}{
		{name: "judge", after: func(r diffRun) diffRun { r.judge = llm; return r }, warning: "judge"},
		{name: "relevance threshold", after: func(r diffRun) diffRun { r.threshold = 2; return r }, warning: "relevance threshold"},
		{name: "judgments source", after: func(r diffRun) diffRun { r.judgments = "trec/annotations.llm-cli.yaml"; return r }, warning: "judgments"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			after := tc.after(base)
			after.runID = "b"

			var out bytes.Buffer
			printDiff(&out, diffReport(base), diffReport(after))
			got := out.String()

			warningAt := strings.Index(got, "WARNING")
			require.NotEqual(t, -1, warningAt, got)
			assert.Less(t, warningAt, strings.Index(got, "--- Job:"))
			assert.Contains(t, got[warningAt:strings.Index(got, "--- Job:")], tc.warning)
		})
	}
}

func TestPrintDiff_NoScoringWarningForIdenticallyScoredRuns(t *testing.T) {
	run := diffRun{runID: "a", kValues: []int{10}, ndcg: map[int]float64{10: 0.6}, threshold: 1, judge: &meta.Judge{Strategy: "lexical"}, judgments: "trec/annotations.lexical.yaml"}
	after := run
	after.runID = "b"

	var out bytes.Buffer
	printDiff(&out, diffReport(run), diffReport(after))

	assert.NotContains(t, out.String(), "WARNING")
}
