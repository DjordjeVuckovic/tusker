package suite

import (
	"fmt"
)

// QueryTemplate is a statement shared by many queries, written in the syntax
// of the engines that read it. Args name the params it takes.
type QueryTemplate struct {
	ID    string   `yaml:"id"`
	Args  []string `yaml:"args,omitempty"`
	Query string   `yaml:"query"`
}

type TemplateParams map[string]any

func (t *QueryTemplate) Validate() error {
	if t.ID == "" {
		return fmt.Errorf("template has no id")
	}
	if t.Query == "" {
		return fmt.Errorf("template %q has no query", t.ID)
	}
	return nil
}

func valuesInOrder(names []string, params TemplateParams) ([]any, error) {
	values := make([]any, 0, len(names))
	var missing []string
	for _, name := range names {
		value, ok := params[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		values = append(values, value)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing params: %v", missing)
	}
	return values, nil
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
