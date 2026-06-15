package report

import (
	"fmt"
	"math"

	"github.com/DjordjeVuckovic/tusker/internal/bench/meta"
	"github.com/DjordjeVuckovic/tusker/internal/bench/metrics"
	"github.com/DjordjeVuckovic/tusker/internal/bench/runner"
	"github.com/DjordjeVuckovic/tusker/internal/bench/spec"
	"github.com/DjordjeVuckovic/tusker/internal/bench/version"
)

type GenerateOptions struct {
	Spec   *spec.BenchSpec
	Corpus CorpusInfo
}

func Generate(br *runner.BenchmarkResult, opts *GenerateOptions) *Report {
	r := &Report{
		SchemaVersion: version.SchemaVersion,
		Provenance:    meta.New("run"),
		Environment: RunEnvironment{
			Engines:  make(map[string]EngineInfo),
			Platform: NewPlatformInfo(),
		},
		Config: ReportConfig{
			KValues:            br.Config.KValues,
			RelevanceThreshold: br.Config.RelevanceThreshold,
		},
	}

	if opts != nil {
		if opts.Spec != nil {
			for name, eng := range opts.Spec.Engines {
				r.Environment.Engines[name] = EngineInfo{
					Type:       eng.Type,
					Connection: maskConnection(eng.Connection),
				}
			}
		}
		r.Environment.Corpus = opts.Corpus
	}

	for _, jr := range br.Jobs {
		r.Jobs = append(r.Jobs, generateJobReport(jr, br.Config.KValues))
	}

	return r
}

func maskConnection(conn string) string {
	if len(conn) > 50 {
		return conn[:20] + "..." + conn[len(conn)-20:]
	}
	return conn
}

func generateJobReport(jr *runner.JobResult, kValues []int) JobReport {
	report := JobReport{JobName: jr.JobName}

	for _, qID := range jr.QueryOrder {
		engineResults := jr.Results[qID]
		for _, engName := range jr.EngineNames {
			qr, ok := engineResults[engName]
			if !ok {
				continue
			}
			entry := Entry{
				QueryID:      qr.QueryID,
				JobName:      jr.JobName,
				EngineName:   qr.EngineName,
				Judged:       qr.Scores.Judged,
				NDCG:         qr.Scores.NDCG,
				Precision:    qr.Scores.Precision,
				Recall:       qr.Scores.Recall,
				F1:           qr.Scores.F1,
				AP:           qr.Scores.AP,
				RR:           qr.Scores.RR,
				Bpref:        qr.Scores.Bpref,
				TotalMatches: qr.TotalMatches,
				Latency:      fromRunnerLatencyStats(qr.Latency),
			}
			if qr.Error != nil {
				entry.Error = qr.Error.Error()
			}
			report.PerQuery = append(report.PerQuery, entry)
		}
	}

	report.Aggregated = aggregate(jr, kValues)
	report.Significance = computeSignificance(jr, kValues)
	return report
}

func aggregate(jr *runner.JobResult, kValues []int) []AggregatedEntry {
	entries := make([]AggregatedEntry, 0, len(jr.EngineNames))

	for _, engName := range jr.EngineNames {
		agg := AggregatedEntry{
			EngineName: engName,
			NDCG:       make(map[int]float64, len(kValues)),
			NDCGStddev: make(map[int]float64, len(kValues)),
			Precision:  make(map[int]float64, len(kValues)),
			Recall:     make(map[int]float64, len(kValues)),
			F1:         make(map[int]float64, len(kValues)),
		}

		// Collect per-query NDCG samples for stddev computation.
		ndcgSamples := make(map[int][]float64, len(kValues))
		var allStats []runner.LatencyStats

		for _, qID := range jr.QueryOrder {
			qr, ok := jr.Results[qID][engName]
			if !ok {
				continue
			}
			agg.QueryCount++

			if qr.Error != nil {
				agg.ErrorCount++
				continue
			}

			allStats = append(allStats, qr.Latency)

			if !qr.Scores.Judged {
				continue
			}

			agg.JudgedCount++
			agg.MAP += qr.Scores.AP
			agg.MRR += qr.Scores.RR
			agg.MBpref += qr.Scores.Bpref

			for _, k := range kValues {
				v := qr.Scores.NDCG[k]
				agg.NDCG[k] += v
				ndcgSamples[k] = append(ndcgSamples[k], v)
				agg.Precision[k] += qr.Scores.Precision[k]
				agg.Recall[k] += qr.Scores.Recall[k]
				agg.F1[k] += qr.Scores.F1[k]
			}
		}

		if len(allStats) > 0 {
			agg.Latency = fromRunnerLatencyStats(runner.AggregateLatencyStats(allStats))
		}

		if agg.JudgedCount > 0 {
			n := float64(agg.JudgedCount)
			agg.MAP /= n
			agg.MRR /= n
			agg.MBpref /= n
			for _, k := range kValues {
				agg.NDCG[k] /= n
				agg.Precision[k] /= n
				agg.Recall[k] /= n
				agg.F1[k] /= n
				if samples := ndcgSamples[k]; len(samples) > 1 {
					agg.NDCGStddev[k] = sampleStddev(samples)
				}
			}
		}

		entries = append(entries, agg)
	}

	return entries
}

// computeSignificance runs pairwise Wilcoxon signed-rank tests for every
// engine pair in the job, for NDCG@K (all K values), MAP, and MRR.
// Only judged queries where both engines have scores are included.
func computeSignificance(jr *runner.JobResult, kValues []int) []PairwiseSignificance {
	engNames := jr.EngineNames
	var results []PairwiseSignificance

	for i := 0; i < len(engNames); i++ {
		for j := i + 1; j < len(engNames); j++ {
			engA, engB := engNames[i], engNames[j]

			// Collect paired per-query scores for all metrics at once.
			var mapA, mapB, mrrA, mrrB []float64
			ndcgA := make(map[int][]float64, len(kValues))
			ndcgB := make(map[int][]float64, len(kValues))

			for _, qID := range jr.QueryOrder {
				qrA, okA := jr.Results[qID][engA]
				qrB, okB := jr.Results[qID][engB]
				if !okA || !okB || !qrA.Scores.Judged || !qrB.Scores.Judged {
					continue
				}
				mapA = append(mapA, qrA.Scores.AP)
				mapB = append(mapB, qrB.Scores.AP)
				mrrA = append(mrrA, qrA.Scores.RR)
				mrrB = append(mrrB, qrB.Scores.RR)
				for _, k := range kValues {
					ndcgA[k] = append(ndcgA[k], qrA.Scores.NDCG[k])
					ndcgB[k] = append(ndcgB[k], qrB.Scores.NDCG[k])
				}
			}

			for _, k := range kValues {
				if res := metrics.Wilcoxon(ndcgA[k], ndcgB[k]); res != nil {
					results = append(results, PairwiseSignificance{
						EngineA: engA, EngineB: engB,
						Metric: fmt.Sprintf("NDCG@%d", k),
						W:      res.W, P: res.P, Stars: res.Stars,
					})
				}
			}
			if res := metrics.Wilcoxon(mapA, mapB); res != nil {
				results = append(results, PairwiseSignificance{
					EngineA: engA, EngineB: engB, Metric: "MAP",
					W: res.W, P: res.P, Stars: res.Stars,
				})
			}
			if res := metrics.Wilcoxon(mrrA, mrrB); res != nil {
				results = append(results, PairwiseSignificance{
					EngineA: engA, EngineB: engB, Metric: "MRR",
					W: res.W, P: res.P, Stars: res.Stars,
				})
			}
		}
	}
	return results
}

func sampleStddev(vals []float64) float64 {
	if len(vals) < 2 {
		return 0
	}
	mean := 0.0
	for _, v := range vals {
		mean += v
	}
	mean /= float64(len(vals))
	variance := 0.0
	for _, v := range vals {
		d := v - mean
		variance += d * d
	}
	return math.Sqrt(variance / float64(len(vals)-1))
}
