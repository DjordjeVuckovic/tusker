package reader

import (
	"fmt"
	"io"

	"github.com/DjordjeVuckovic/tusker/pkg/apis/datamapping"
	"gopkg.in/yaml.v3"
)

type YAMLConfigLoader struct {
	reader io.Reader
}

func NewYAMLConfigLoader(reader io.Reader) *YAMLConfigLoader {
	return &YAMLConfigLoader{
		reader: reader,
	}
}

// Load decodes the mapping and fails on any key the schema does not define,
// so a misspelt key cannot silently fall back to a default.
func (cl *YAMLConfigLoader) Load(validate bool) (*datamapping.DataMapper, error) {
	decoder := yaml.NewDecoder(cl.reader)
	decoder.KnownFields(true)
	var mapping datamapping.DataMapper
	if err := decoder.Decode(&mapping); err != nil {
		return nil, fmt.Errorf("decode mapping: %w", err)
	}
	if validate {
		if err := mapping.Validate(); err != nil {
			return nil, err
		}
	}
	return &mapping, nil
}
