package suite

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// QueryTemplate is a statement shared by many queries. Args name the param
// behind each of a Postgres statement's $1 … $n, in order, or the params an
// Elasticsearch search template reads.
type QueryTemplate struct {
	ID    string   `yaml:"id"`
	Args  []string `yaml:"args,omitempty"`
	Query string   `yaml:"query"`
}

type TemplateParams map[string]any

var (
	positionalPlaceholder = regexp.MustCompile(`\$(\d+)`)
	mustacheTag           = regexp.MustCompile(`\{\{\{?\s*([#^/]?)\s*([A-Za-z_][\w.]*)[^}]*\}?\}\}`)
	mustacheFunctionArg   = regexp.MustCompile(`\{\{#(?:toJson|join)[^}]*\}\}\s*([A-Za-z_][\w.]*)\s*\{\{/(?:toJson|join)\}\}`)
)

// mustacheFunctions are the lambdas Elasticsearch adds to Mustache. A section
// named after one calls it rather than reading a param.
var mustacheFunctions = []string{"toJson", "join", "url"}

func (t *QueryTemplate) Validate() error {
	if t.ID == "" {
		return fmt.Errorf("template has no id")
	}
	if t.Query == "" {
		return fmt.Errorf("template %q has no query", t.ID)
	}
	if err := checkArgsFitStatement(t.Query, t.Args); err != nil {
		return fmt.Errorf("template %q: %w", t.ID, err)
	}
	return nil
}

func checkArgsFitStatement(statement string, args []string) error {
	if err := checkArgsCoverPlaceholders(statement, args); err != nil {
		return err
	}
	return checkArgsMatchMustacheNames(statement, args)
}

func checkArgsCoverPlaceholders(statement string, args []string) error {
	highest := 0
	for _, m := range positionalPlaceholder.FindAllStringSubmatch(statement, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return fmt.Errorf("placeholder $%s: %w", m[1], err)
		}
		highest = max(highest, n)
	}
	if highest > len(args) {
		return fmt.Errorf("statement uses $%d but args names only %d: %v", highest, len(args), args)
	}
	return nil
}

// checkArgsMatchMustacheNames requires args to be exactly the names a search
// template reads, since Mustache renders a name it is not given as empty text.
func checkArgsMatchMustacheNames(source string, args []string) error {
	names := mustacheNames(source)
	if len(names) == 0 {
		return nil
	}
	for _, name := range names {
		if !slices.Contains(args, name) {
			return fmt.Errorf("source reads {{%s}} but args leaves it out: %v", name, args)
		}
	}
	for _, arg := range args {
		if !slices.Contains(names, arg) {
			return fmt.Errorf("args names %q but the source never reads it", arg)
		}
	}
	return nil
}

func mustacheNames(source string) []string {
	var names []string
	for _, m := range mustacheFunctionArg.FindAllStringSubmatch(source, -1) {
		names = append(names, topLevelName(m[1]))
	}
	for _, m := range mustacheTag.FindAllStringSubmatch(source, -1) {
		sigil, name := m[1], m[2]
		if sigil == "/" || slices.Contains(mustacheFunctions, name) {
			continue
		}
		names = append(names, topLevelName(name))
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func topLevelName(path string) string {
	name, _, _ := strings.Cut(path, ".")
	return name
}

// positionalArgs looks up each named arg in params and returns the values as
// text, in order. Text leaves typing to the statement's own casts.
func positionalArgs(names []string, params TemplateParams) ([]any, error) {
	args := make([]any, 0, len(names))
	var missing []string
	for _, name := range names {
		value, ok := params[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		args = append(args, argText(value))
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing params: %v", missing)
	}
	return args, nil
}

func argText(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case []float32:
		return FormatVector(val)
	default:
		return fmt.Sprint(v)
	}
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

func (r *TemplateRegistry) List() []string {
	ids := make([]string, 0, len(r.templates))
	for id := range r.templates {
		ids = append(ids, id)
	}
	return ids
}
