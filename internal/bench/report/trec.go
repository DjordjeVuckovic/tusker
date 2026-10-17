package report

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/google/uuid"
)

func rankDerivedScores(ranked []uuid.UUID) []RankedDoc {
	if len(ranked) == 0 {
		return nil
	}
	docs := make([]RankedDoc, len(ranked))
	for i, docID := range ranked {
		docs[i] = RankedDoc{DocID: docID, Score: float64(len(ranked) - i)}
	}
	return docs
}

// WriteTRECRun writes one engine's rankings in a job as a TREC run file,
// "qid Q0 docid rank score tag" per line, tagged with the engine name. A query
// the engine failed has no lines, which trec_eval -c scores as 0.
func WriteTRECRun(w io.Writer, job *JobReport, engineName string) error {
	bw := bufio.NewWriter(w)
	for _, e := range job.PerQuery {
		if e.EngineName != engineName || e.Error != "" {
			continue
		}
		for i, doc := range e.Ranking {
			fmt.Fprintf(bw, "%s Q0 %s %d %s %s\n",
				e.QueryID, doc.DocID, i+1, strconv.FormatFloat(doc.Score, 'g', -1, 64), engineName)
		}
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("write trec run for %s: %w", engineName, err)
	}
	return nil
}

// ErrNoRankings is returned for a report written before rankings were recorded.
var ErrNoRankings = errors.New("report records no rankings; re-run bench run to export a TREC run")

// WriteTRECRuns writes one run file per job and engine into dir, named
// <job>.<engine>.run, since trec_eval scores each file as a single run.
func WriteTRECRuns(r *Report, dir string) ([]string, error) {
	if !hasRankings(r) {
		return nil, ErrNoRankings
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create run directory %s: %w", dir, err)
	}
	var paths []string
	for i := range r.Jobs {
		job := &r.Jobs[i]
		for _, agg := range job.Aggregated {
			path := filepath.Join(dir, job.JobName+"."+agg.EngineName+".run")
			if err := writeTRECRunFile(path, job, agg.EngineName); err != nil {
				return paths, err
			}
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func writeTRECRunFile(path string, job *JobReport, engineName string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create run file %s: %w", path, err)
	}
	if err := WriteTRECRun(f, job, engineName); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close run file %s: %w", path, err)
	}
	return nil
}

func hasRankings(r *Report) bool {
	for _, job := range r.Jobs {
		for _, e := range job.PerQuery {
			if len(e.Ranking) > 0 {
				return true
			}
		}
	}
	return false
}
