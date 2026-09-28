package suite

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// ReservedQueryVectorParam is the template/param placeholder replaced at run
// time with the live query embedding (the {{precomputed}} blocker). The
// pool/run pipeline embeds the query once and injects it under this name; the
// PG vector template wraps it as '[...]'::vector and the ES knn body inlines it
// as a JSON array.
const ReservedQueryVectorParam = "precomputed"

// EmbeddingModelParam names the engine-level param carrying the embedding model
// an arm is measured against. article_embeddings is keyed (article_id,
// model_name), so a vector template filters on it and the runner refuses to run
// an engine whose declared value disagrees with the model embedding the query.
const EmbeddingModelParam = "embedding_model"

type TestSuite struct {
	SchemaVersion int              `yaml:"schema_version"`
	ID            string           `yaml:"id"`
	Name          string           `yaml:"name,omitempty"`
	Description   string           `yaml:"description,omitempty"`
	Version       string           `yaml:"version"`
	Corpus        *Corpus          `yaml:"corpus,omitempty"`
	Templates     []*QueryTemplate `yaml:"templates,omitempty"`
	Queries       []Query          `yaml:"queries"`
}

// Corpus records the dataset the suite targets. Lets a report attest "this
// was scored against the news_hunter_articles index, snapshot 2026-05-10".
type Corpus struct {
	Name       string `yaml:"name"`
	Source     string `yaml:"source,omitempty"`
	SnapshotAt string `yaml:"snapshot_at,omitempty"`
}

type Query struct {
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
	// Category drives per-category aggregation and the judge's grading rule.
	Category  string                 `yaml:"category,omitempty"`
	Engines   map[string]EngineQuery `yaml:"engines"`
	Judgments []RelevanceJudgment    `yaml:"judgments"`
}

type EngineQuery struct {
	Query    string         `yaml:"query,omitempty"`
	File     string         `yaml:"file,omitempty"`
	Template string         `yaml:"template,omitempty"`
	Params   TemplateParams `yaml:"params,omitempty"`
}

func (eq *EngineQuery) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		eq.Query = value.Value
		return nil
	}
	type plain EngineQuery
	return value.Decode((*plain)(eq))
}

// ResolveOptions addresses one engine's query inside a suite query and carries
// the three param layers, widest first: engine Defaults, the query's own
// params, then Extra.
type ResolveOptions struct {
	// Engine names the per-query block to read. Under engine aliasing this is
	// the engine that owns the queries, which need not be the one being
	// measured.
	Engine   string
	Registry *TemplateRegistry
	SuiteDir string
	// Defaults are the running engine's declared params, the widest layer.
	Defaults TemplateParams
	// Extra are run-time params absent from the suite, chiefly the live query
	// vector under ReservedQueryVectorParam.
	Extra TemplateParams
}

// Resolve renders the engine query, merging the param layers so a narrower one
// wins: engine defaults, then the query's own params, then run-time extras.
func (eq *EngineQuery) Resolve(opts ResolveOptions) (*ResolvedQuery, error) {
	params := mergeParams(opts.Defaults, eq.Params, opts.Extra)
	if eq.Template != "" {
		if opts.Registry == nil {
			return nil, fmt.Errorf("template %q referenced but no registry available", eq.Template)
		}
		return opts.Registry.RenderQuery(eq.Template, params, opts.SuiteDir)
	}
	if eq.File != "" {
		path := eq.File
		if !filepath.IsAbs(path) {
			path = filepath.Join(opts.SuiteDir, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read query file %q: %w", eq.File, err)
		}
		return resolveInline(string(data), params)
	}
	return resolveInline(eq.Query, params)
}

// resolveInline substitutes params into an inline/file query and rejects any
// {{...}} left unresolved. Without this, an un-injected placeholder (e.g.
// {{precomputed}} when no embedder ran) ships verbatim to the engine — ES then
// parses the literal "{" as an object and returns a cryptic START_OBJECT 400.
// Templates already fail loudly via Render; this gives inline queries parity.
func resolveInline(s string, params TemplateParams) (*ResolvedQuery, error) {
	s = substituteParams(s, params)
	if missing := findMissingPlaceholders(s); len(missing) > 0 {
		return nil, fmt.Errorf("query has unresolved placeholders: %v", missing)
	}
	return &ResolvedQuery{Query: s}, nil
}

// mergeParams overlays layers left to right into a fresh map, so a narrower
// layer wins and no caller's map is aliased — engine params are owned by the
// spec and shared by every query the engine runs.
func mergeParams(layers ...TemplateParams) TemplateParams {
	out := TemplateParams{}
	for _, layer := range layers {
		for k, v := range layer {
			out[k] = v
		}
	}
	return out
}

// substituteParams replaces {{key}} for each key in params. Inline/file queries
// aren't template-rendered, so this is how they receive params; only the
// provided keys are touched, leaving any other braces untouched.
func substituteParams(s string, params TemplateParams) string {
	for k, v := range params {
		s = strings.ReplaceAll(s, "{{"+k+"}}", formatValue(v))
	}
	return s
}

type ResolvedQuery struct {
	Query string
}

type RelevanceJudgment struct {
	DocID     uuid.UUID `yaml:"doc_id"`
	Relevance int       `yaml:"relevance"`
}

func (q *Query) JudgmentMap() map[uuid.UUID]int {
	m := make(map[uuid.UUID]int, len(q.Judgments))
	for _, j := range q.Judgments {
		m[j.DocID] = j.Relevance
	}
	return m
}

// InjectJudgments sets the per-query Judgments slice from a flat map produced
// by the CLI layer after loading an annotations file. Replaces the loader-side
// auto-injection of v0; keeps the suite YAML focused on queries only.
func (ls *LoadedSuite) InjectJudgments(byQuery map[string][]RelevanceJudgment) {
	for i := range ls.Suite.Queries {
		if js, ok := byQuery[ls.Suite.Queries[i].ID]; ok {
			ls.Suite.Queries[i].Judgments = js
		}
	}
}

// ResolveEngineQuery renders the block named by opts.Engine, or returns a nil
// query when the suite declares none for it.
func (q *Query) ResolveEngineQuery(opts ResolveOptions) (*ResolvedQuery, error) {
	eq, ok := q.Engines[opts.Engine]
	if !ok {
		return nil, nil
	}
	return eq.Resolve(opts)
}

// NeedsQueryVector reports whether any engine query references the reserved
// query-vector placeholder, so the pipeline knows to embed the query.
func (q *Query) NeedsQueryVector() bool {
	token := "{{" + ReservedQueryVectorParam + "}}"
	for _, eq := range q.Engines {
		if strings.Contains(eq.Query, token) {
			return true
		}
		for _, v := range eq.Params {
			if s, ok := v.(string); ok && strings.Contains(s, token) {
				return true
			}
		}
	}
	return false
}

// FormatVector renders a float vector as a bracketed array literal — valid both
// as a pgvector input ('[...]'::vector) and as a JSON array for ES knn.
func FormatVector(vec []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range vec {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
