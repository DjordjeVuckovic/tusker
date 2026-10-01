package suite

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type QueryTemplate struct {
	ID    string `yaml:"id"`
	Query string `yaml:"query"`
}

type TemplateParams map[string]any

// Dialect is how a bound {{$name}} value reaches an engine.
type Dialect string

const (
	// DialectPostgres renders a bound value as a positional $N argument.
	DialectPostgres Dialect = "postgres"
	// DialectJSON inlines a bound value as a JSON-encoded string.
	DialectJSON Dialect = "json"
)

func (d Dialect) validate() error {
	switch d {
	case DialectPostgres, DialectJSON:
		return nil
	default:
		return fmt.Errorf("unknown query dialect %q", d)
	}
}

var (
	placeholderRegex      = regexp.MustCompile(`\{\{(\w+)\}\}`)
	boundPlaceholderRegex = regexp.MustCompile(`\{\{\$(\w+)\}\}`)
)

// Render substitutes structural {{name}} params as text and binds value
// {{$name}} params the way dialect requires.
func (t *QueryTemplate) Render(params TemplateParams, dialect Dialect) (*ResolvedQuery, error) {
	// Substitute repeatedly so placeholders introduced by a param's value also
	// resolve — e.g. a query maps `embedding: "{{precomputed}}"` and the run-time
	// vector is supplied under `precomputed`. Bounded to avoid self-referential
	// cycles.
	result := t.Query
	pasted := map[string]bool{}
	for i := 0; i < 5; i++ {
		next := placeholderRegex.ReplaceAllStringFunc(result, func(match string) string {
			key := match[2 : len(match)-2]
			if val, ok := params[key]; ok {
				pasted[key] = true
				return formatValue(val)
			}
			return match
		})
		if next == result {
			break
		}
		result = next
	}

	missing := findMissingPlaceholders(result)
	if len(missing) > 0 {
		return nil, fmt.Errorf("template %q missing params: %v", t.ID, missing)
	}

	if name, found := boundParamAlsoPasted(result, pasted); found {
		return nil, fmt.Errorf("template %q uses param %q both as {{$%s}} and as {{%s}}: bind it everywhere", t.ID, name, name, name)
	}

	resolved, err := bindValues(result, params, dialect)
	if err != nil {
		return nil, fmt.Errorf("template %q: %w", t.ID, err)
	}
	return resolved, nil
}

// boundParamAlsoPasted finds a param query binds as {{$name}} whose value was
// also pasted as {{name}}. The pasted copy puts the text back into the query
// unescaped, which is what binding exists to prevent.
func boundParamAlsoPasted(query string, pasted map[string]bool) (string, bool) {
	for _, m := range boundPlaceholderRegex.FindAllStringSubmatch(query, -1) {
		if pasted[m[1]] {
			return m[1], true
		}
	}
	return "", false
}

// bindValues runs once, after every structural placeholder is resolved, so a
// bound value is never scanned for placeholders of its own.
func bindValues(query string, params TemplateParams, dialect Dialect) (*ResolvedQuery, error) {
	if !boundPlaceholderRegex.MatchString(query) {
		return &ResolvedQuery{Query: query}, nil
	}
	if err := dialect.validate(); err != nil {
		return nil, fmt.Errorf("bound params: %w", err)
	}

	var (
		args     []any
		position = map[string]int{}
		missing  []string
	)
	bound := boundPlaceholderRegex.ReplaceAllStringFunc(query, func(match string) string {
		name := match[3 : len(match)-2]
		val, ok := params[name]
		if !ok {
			if !slices.Contains(missing, name) {
				missing = append(missing, name)
			}
			return match
		}
		text := formatValue(val)
		if dialect == DialectJSON {
			encoded, _ := json.Marshal(text)
			return string(encoded)
		}
		n, seen := position[name]
		if !seen {
			args = append(args, text)
			n = len(args)
			position[name] = n
		}
		return "$" + strconv.Itoa(n)
	})
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing params: %v", missing)
	}
	return &ResolvedQuery{Query: bound, Args: args}, nil
}

func (t *QueryTemplate) Validate() error {
	if t.ID == "" {
		return fmt.Errorf("template has no id")
	}
	if t.Query == "" {
		return fmt.Errorf("template %q has no query", t.ID)
	}
	return nil
}

func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(val)
	case []string:
		return strings.Join(val, ", ")
	case []any:
		strs := make([]string, len(val))
		for i, item := range val {
			strs[i] = formatValue(item)
		}
		return strings.Join(strs, ", ")
	default:
		return fmt.Sprintf("%v", v)
	}
}

func findMissingPlaceholders(s string) []string {
	matches := placeholderRegex.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var missing []string
	for _, m := range matches {
		if len(m) > 1 && !seen[m[1]] {
			seen[m[1]] = true
			missing = append(missing, m[1])
		}
	}
	return missing
}

type TemplateRegistry struct {
	templates map[string]*QueryTemplate
}

func NewTemplateRegistry() *TemplateRegistry {
	return &TemplateRegistry{
		templates: make(map[string]*QueryTemplate),
	}
}

func (r *TemplateRegistry) Register(t *QueryTemplate) error {
	if err := t.Validate(); err != nil {
		return err
	}
	if _, exists := r.templates[t.ID]; exists {
		return fmt.Errorf("template %q already registered", t.ID)
	}
	r.templates[t.ID] = t
	return nil
}

func (r *TemplateRegistry) Get(id string) (*QueryTemplate, bool) {
	t, ok := r.templates[id]
	return t, ok
}

func (r *TemplateRegistry) RenderQuery(templateID string, params TemplateParams, dialect Dialect) (*ResolvedQuery, error) {
	t, ok := r.Get(templateID)
	if !ok {
		return nil, fmt.Errorf("template %q not found", templateID)
	}
	return t.Render(params, dialect)
}

func (r *TemplateRegistry) List() []string {
	ids := make([]string, 0, len(r.templates))
	for id := range r.templates {
		ids = append(ids, id)
	}
	return ids
}
