package suite

import (
	"fmt"
	"regexp"
	"strconv"
)

// QueryTemplate is a statement shared by many queries. Args name the param
// behind each of a Postgres statement's $1 … $n, in order.
type QueryTemplate struct {
	ID    string   `yaml:"id"`
	Args  []string `yaml:"args,omitempty"`
	Query string   `yaml:"query"`
}

type TemplateParams map[string]any

var positionalPlaceholder = regexp.MustCompile(`\$(\d+)`)

func (t *QueryTemplate) Validate() error {
	if t.ID == "" {
		return fmt.Errorf("template has no id")
	}
	if t.Query == "" {
		return fmt.Errorf("template %q has no query", t.ID)
	}
	if err := checkArgsCoverStatement(t.Query, t.Args); err != nil {
		return fmt.Errorf("template %q: %w", t.ID, err)
	}
	return nil
}

func checkArgsCoverStatement(statement string, args []string) error {
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
