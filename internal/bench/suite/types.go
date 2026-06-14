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
	ID          string                 `yaml:"id"`
	Description string                 `yaml:"description"`
	Engines     map[string]EngineQuery `yaml:"engines"`
	Judgments   []RelevanceJudgment    `yaml:"judgments"`
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

// Resolve renders the engine query. extra supplies run-time params (e.g. the
// live query vector under ReservedQueryVectorParam) that aren't in the suite:
// they are merged into template params and substituted into inline/file
// queries. extra may be nil.
func (eq *EngineQuery) Resolve(registry *TemplateRegistry, suiteDir string, extra TemplateParams) (*ResolvedQuery, error) {
	if eq.Template != "" {
		if registry == nil {
			return nil, fmt.Errorf("template %q referenced but no registry available", eq.Template)
		}
		return registry.RenderQuery(eq.Template, mergeParams(eq.Params, extra), suiteDir)
	}
	if eq.File != "" {
		path := eq.File
		if !filepath.IsAbs(path) {
			path = filepath.Join(suiteDir, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read query file %q: %w", eq.File, err)
		}
		return resolveInline(string(data), extra)
	}
	return resolveInline(eq.Query, extra)
}

// resolveInline substitutes extra into an inline/file query and rejects any
// {{...}} left unresolved. Without this, an un-injected placeholder (e.g.
// {{precomputed}} when no embedder ran) ships verbatim to the engine — ES then
// parses the literal "{" as an object and returns a cryptic START_OBJECT 400.
// Templates already fail loudly via Render; this gives inline queries parity.
func resolveInline(s string, extra TemplateParams) (*ResolvedQuery, error) {
	s = substituteExtra(s, extra)
	if missing := findMissingPlaceholders(s); len(missing) > 0 {
		return nil, fmt.Errorf("query has unresolved placeholders: %v", missing)
	}
	return &ResolvedQuery{Query: s}, nil
}

// mergeParams overlays extra onto base without mutating either.
func mergeParams(base, extra TemplateParams) TemplateParams {
	if len(extra) == 0 {
		return base
	}
	out := make(TemplateParams, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// substituteExtra replaces {{key}} for each key in extra. Inline/file queries
// aren't template-rendered, so this is how they receive run-time params; only
// the provided keys are touched, leaving any other braces untouched.
func substituteExtra(s string, extra TemplateParams) string {
	for k, v := range extra {
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

func (q *Query) ResolveEngineQuery(engine string, registry *TemplateRegistry, suiteDir string, extra TemplateParams) (*ResolvedQuery, error) {
	eq, ok := q.Engines[engine]
	if !ok {
		return nil, nil
	}
	return eq.Resolve(registry, suiteDir, extra)
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
