package datapackage

import (
	"errors"
	"fmt"
	"strings"

	"github.com/DjordjeVuckovic/tusker/pkg/apis/datamapping"
)

// validate checks what every package needs before its resources can be
// trusted. Write also requires a name, which the v2 profile leaves optional.
func (p Package) validate() error {
	if p.IDStrategy != nil {
		if _, err := datamapping.ParseIdStrategyKind(string(p.IDStrategy.Kind)); err != nil {
			return fmt.Errorf("package %s: %w", p.Name, err)
		}
	}
	if len(p.Resources) == 0 {
		return fmt.Errorf("package %s has no resources", p.Name)
	}
	seen := make(map[string]bool, len(p.Resources))
	for _, r := range p.Resources {
		if r.Name == "" {
			return fmt.Errorf("package %s: resource at %q has no name", p.Name, r.Path)
		}
		if seen[r.Name] {
			return fmt.Errorf("package %s: resource name %q is used twice", p.Name, r.Name)
		}
		seen[r.Name] = true
		if r.Format == "" {
			return fmt.Errorf("package %s: resource %s has no format", p.Name, r.Name)
		}
		if err := validateResourcePath(r.Path); err != nil {
			return fmt.Errorf("package %s: resource %s: %w", p.Name, r.Name, err)
		}
	}
	return nil
}

// validateResourcePath accepts only slash-separated paths that stay under the
// descriptor's directory, so a fetched descriptor can never point a reader at
// an arbitrary local file or another host.
func validateResourcePath(p string) error {
	switch {
	case p == "":
		return errors.New("empty path")
	case strings.ContainsAny(p[:1], "./~"):
		return fmt.Errorf("path %q must not start with '.', '/' or '~'", p)
	case strings.Contains(p, `\`):
		return fmt.Errorf("path %q must use '/' separators", p)
	case strings.Contains(p, ":"):
		return fmt.Errorf("path %q must be relative to the descriptor", p)
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == ".." || segment == "." || segment == "" {
			return fmt.Errorf("path %q must not contain empty, '.' or '..' segments", p)
		}
	}
	return nil
}
