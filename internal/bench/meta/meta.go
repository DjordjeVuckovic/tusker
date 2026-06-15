// Package meta defines the provenance block embedded in every bench artifact.
// Mirror of the "report card" convention from Anthropic/OpenAI eval cookbooks:
// every output should self-attest to who/what/when produced it.
package meta

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/bench/version"
)

// Meta is embedded in pool/annotations/report artifacts. Fields are
// intentionally additive — readers tolerate unknown keys, writers omit empty.
type Meta struct {
	RunID       string    `yaml:"run_id" json:"run_id"`
	Tool        string    `yaml:"tool" json:"tool"`
	GeneratedAt time.Time `yaml:"generated_at" json:"generated_at"`

	// Identity of the track/spec/suite that produced this artifact.
	SpecID       string `yaml:"spec_id,omitempty" json:"spec_id,omitempty"`
	SuiteID      string `yaml:"suite_id,omitempty" json:"suite_id,omitempty"`
	SuiteVersion string `yaml:"suite_version,omitempty" json:"suite_version,omitempty"`

	// Pool-only fields.
	PoolDepth int      `yaml:"pool_depth,omitempty" json:"pool_depth,omitempty"`
	Engines   []string `yaml:"engines,omitempty" json:"engines,omitempty"`

	// Judgment-only fields.
	Strategy           string `yaml:"strategy,omitempty" json:"strategy,omitempty"`
	JudgeModel         string `yaml:"judge_model,omitempty" json:"judge_model,omitempty"`
	JudgePromptVersion string `yaml:"judge_prompt_version,omitempty" json:"judge_prompt_version,omitempty"`
	PoolRef            string `yaml:"pool_ref,omitempty" json:"pool_ref,omitempty"`
	GradedCount        int    `yaml:"graded_count,omitempty" json:"graded_count,omitempty"`
	RelevanceScale     []int  `yaml:"relevance_scale,omitempty" json:"relevance_scale,omitempty"`

	// Report-only fields.
	Sources *Sources `yaml:"sources,omitempty" json:"sources,omitempty"`
}

// Sources is embedded in a Report's meta to attest which on-disk artifacts
// were consumed to compute the metrics in that report.
type Sources struct {
	Spec       string `yaml:"spec,omitempty" json:"spec,omitempty"`
	Suite      string `yaml:"suite,omitempty" json:"suite,omitempty"`
	Pool       string `yaml:"pool,omitempty" json:"pool,omitempty"`
	Judgments  string `yaml:"judgments,omitempty" json:"judgments,omitempty"`
	JudgesPath string `yaml:"judgments_path,omitempty" json:"judgments_path,omitempty"`
}

// New returns a freshly-stamped Meta with tool string + ISO timestamp + run id.
// The kind argument ("pool", "judge", "run") is embedded in the run id for
// human readability.
func New(kind string) Meta {
	return Meta{
		RunID:       NewRunID(kind),
		Tool:        version.Tool(),
		GeneratedAt: time.Now().UTC(),
	}
}

// NewRunID generates a sortable identifier of the form
//
//	2026-05-21T14-08-55-judge-7c91a3
//
// Lexicographic order = chronological order. Random suffix avoids collisions
// when two runs of the same kind fire in the same second.
func NewRunID(kind string) string {
	stamp := time.Now().UTC().Format("2006-01-02T15-04-05")
	return fmt.Sprintf("%s-%s-%s", stamp, kind, randHex(3))
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Cryptographic randomness should never fail in practice; if it does,
		// fall back to a fixed marker so the run id is still well-formed.
		return "000000"[:n*2]
	}
	return hex.EncodeToString(b)
}
