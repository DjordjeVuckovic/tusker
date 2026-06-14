package spec

type BenchSpec struct {
	SchemaVersion int               `yaml:"schema_version"`
	ID            string            `yaml:"id"`
	Kind          Kind              `yaml:"kind,omitempty"`
	Description   string            `yaml:"description,omitempty"`
	Defaults      Defaults          `yaml:"defaults,omitempty"`
	Engines       map[string]Engine `yaml:"engines"`
	Metrics       MetricsConfig     `yaml:"metrics"`
	Runs          RunsConfig        `yaml:"runs"`
	Jobs          []Job             `yaml:"jobs"`

	// Warnings collects non-fatal load-time advisories (e.g. kind omitted). It is
	// populated by the loader and never serialized; the CLI prints it once.
	Warnings []string `yaml:"-"`
}

// Kind names the IR paradigm a track measures. It is primarily a taxonomy /
// provenance label (one per track, aligned with the thesis's search
// paradigms); requirements such as "needs an embedder" are derived from it
// rather than declared separately. Optional — an empty kind is valid but the
// loader warns, since it disables the derived preconditions.
type Kind string

const (
	KindFTS        Kind = "fts"
	KindStructured Kind = "structured"
	KindFuzzy      Kind = "fuzzy"
	KindSemantic   Kind = "semantic"
	KindHybrid     Kind = "hybrid"
)

// Valid reports whether k is empty (allowed — kind is optional) or one of the
// known paradigms. An unknown non-empty value is a hard error at load.
func (k Kind) Valid() bool {
	switch k {
	case "", KindFTS, KindStructured, KindFuzzy, KindSemantic, KindHybrid:
		return true
	default:
		return false
	}
}

// RequiresEmbedder reports whether the paradigm needs a live query embedder
// (EMBEDDING_BASE_URL + an embedding-capable engine). Semantic and hybrid
// queries carry the reserved {{precomputed}} vector placeholder; the rest are
// lexical and resolve without one.
func (k Kind) RequiresEmbedder() bool {
	return k == KindSemantic || k == KindHybrid
}

// Defaults supply fallback values that the CLI flags can override. Lets users
// set "this track defaults to lexical judgments and pool depth 100" in one
// place instead of repeating flags.
type Defaults struct {
	PoolDepth int    `yaml:"pool_depth,omitempty"`
	Judgments string `yaml:"judgments,omitempty"` // strategy name OR path
}

type Job struct {
	Name    string   `yaml:"name"`
	Suite   string   `yaml:"suite"`
	Engines []string `yaml:"engines"`
}

type Engine struct {
	Type       string `yaml:"type"`
	Connection string `yaml:"connection"`
	Index      string `yaml:"index,omitempty"`
}

type MetricsConfig struct {
	KValues            []int `yaml:"k_values"`
	MaxK               int   `yaml:"max_k"`
	RelevanceThreshold int   `yaml:"relevance_threshold"`
}

type RunsConfig struct {
	Warmup     int `yaml:"warmup"`
	Iterations int `yaml:"iterations"`
}
