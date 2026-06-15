package report

import (
	"runtime"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/bench/meta"
	"github.com/DjordjeVuckovic/tusker/internal/bench/runner"
)

// Report is the root artifact produced by bench run. It has exactly two
// identity blocks:
//
//   - Provenance (meta.Meta) — who/what/when produced this report and which
//     source artifacts it consumed (spec, suite, pool, judgments).
//   - Environment (RunEnvironment) — runtime context: engines tested, corpus
//     shape, and host platform.
//
// There is no legacy "meta" block — Version and Timestamp lived there but are
// already in Provenance.Tool and Provenance.GeneratedAt.
type Report struct {
	SchemaVersion int            `json:"schema_version"`
	Provenance    meta.Meta      `json:"provenance"`
	Environment   RunEnvironment `json:"environment"`
	Jobs          []JobReport    `json:"jobs"`
	Config        ReportConfig   `json:"config"`
}

// RunEnvironment captures the runtime context of a bench run — engines tested,
// corpus shape, and host platform. Distinct from Provenance (identity/sources)
// and from Jobs (the measured outcomes).
type RunEnvironment struct {
	Engines  map[string]EngineInfo `json:"engines"`
	Corpus   CorpusInfo            `json:"corpus,omitempty"`
	Platform PlatformInfo          `json:"platform"`
}

type EngineInfo struct {
	Type       string `json:"type"`
	Connection string `json:"connection"`
	Version    string `json:"version,omitempty"`
}

type CorpusInfo struct {
	Name      string `json:"name,omitempty"`
	DocCount  int64  `json:"doc_count,omitempty"`
	IndexName string `json:"index_name,omitempty"`
}

type PlatformInfo struct {
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	NumCPU    int    `json:"num_cpu"`
}

func NewPlatformInfo() PlatformInfo {
	return PlatformInfo{
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		NumCPU:    runtime.NumCPU(),
	}
}

type JobReport struct {
	JobName      string
	Aggregated   []AggregatedEntry
	PerQuery     []Entry
	Significance []PairwiseSignificance
}

// PairwiseSignificance is the result of a Wilcoxon signed-rank test comparing
// two engines on one metric across all judged queries in this job. Populated by
// Generate() when ≥4 non-tied paired observations are available.
type PairwiseSignificance struct {
	EngineA string  // reference engine (listed first in spec jobs[].engines)
	EngineB string  // comparison engine
	Metric  string  // e.g. "NDCG@10", "MAP", "MRR"
	W       float64 // Wilcoxon W statistic
	P       float64 // two-tailed p-value
	Stars   string  // "**" p<0.01 | "*" p<0.05 | "" not significant
}

type ReportConfig struct {
	KValues            []int
	RelevanceThreshold int
}

type Entry struct {
	QueryID      string
	JobName      string
	EngineName   string
	Judged       bool
	NDCG         map[int]float64
	Precision    map[int]float64
	Recall       map[int]float64
	F1           map[int]float64
	AP           float64
	RR           float64
	Bpref        float64
	TotalMatches int64
	Latency      LatencyStats
	Error        string
}

type AggregatedEntry struct {
	EngineName  string
	NDCG        map[int]float64
	NDCGStddev  map[int]float64 // sample stddev of per-query NDCG — measures consistency
	Precision   map[int]float64
	Recall      map[int]float64
	F1          map[int]float64
	MAP         float64
	MRR         float64
	MBpref      float64
	Latency     LatencyStats
	QueryCount  int
	JudgedCount int
	ErrorCount  int
}

type LatencyStats struct {
	Min         time.Duration         `json:"min"`
	Max         time.Duration         `json:"max"`
	Mean        time.Duration         `json:"mean"`
	Median      time.Duration         `json:"median"`
	Stddev      time.Duration         `json:"stddev"`
	Percentiles map[int]time.Duration `json:"percentiles"`
	SampleCount int                   `json:"sample_count"`
}

func fromRunnerLatencyStats(s runner.LatencyStats) LatencyStats {
	return LatencyStats{
		Min:         s.Min,
		Max:         s.Max,
		Mean:        s.Mean,
		Median:      s.Median,
		Stddev:      s.Stddev,
		Percentiles: s.Percentiles,
		SampleCount: s.SampleCount,
	}
}

func (s LatencyStats) P50() time.Duration { return s.Percentiles[50] }
func (s LatencyStats) P75() time.Duration { return s.Percentiles[75] }
func (s LatencyStats) P90() time.Duration { return s.Percentiles[90] }
func (s LatencyStats) P95() time.Duration { return s.Percentiles[95] }
func (s LatencyStats) P99() time.Duration { return s.Percentiles[99] }
