package suite

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/DjordjeVuckovic/tusker/internal/bench/version"
	"gopkg.in/yaml.v3"
)

type LoadedSuite struct {
	Suite    *TestSuite
	Registry *TemplateRegistry
	Dir      string
}

func LoadFromFile(path string) (*LoadedSuite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read suite file: %w", err)
	}
	loaded, err := Parse(data)
	if err != nil {
		return nil, err
	}
	loaded.Dir = filepath.Dir(path)
	return loaded, nil
}

func Parse(data []byte) (*LoadedSuite, error) {
	var s TestSuite
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse suite YAML: %w", err)
	}
	if err := version.CheckSchema(s.SchemaVersion, "suite"); err != nil {
		return nil, err
	}
	if s.ID == "" {
		return nil, fmt.Errorf("suite is missing required field: id")
	}
	if len(s.Queries) == 0 {
		return nil, fmt.Errorf("suite has no queries")
	}

	registry := NewTemplateRegistry()
	for _, t := range s.Templates {
		if err := registry.Register(t); err != nil {
			return nil, fmt.Errorf("register template: %w", err)
		}
	}

	for i, q := range s.Queries {
		if q.ID == "" {
			return nil, fmt.Errorf("query at index %d has no id", i)
		}
		if len(q.Engines) == 0 {
			return nil, fmt.Errorf("query %q has no engines", q.ID)
		}
		for engName, eq := range q.Engines {
			if eq.Template != "" {
				if _, ok := registry.Get(eq.Template); !ok {
					return nil, fmt.Errorf("query %q engine %q references unknown template %q", q.ID, engName, eq.Template)
				}
			}
		}
	}

	return &LoadedSuite{Suite: &s, Registry: registry}, nil
}
