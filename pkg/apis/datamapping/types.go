package datamapping

import (
	"fmt"
	"strings"
)

// DataMapper defines field mapping configuration for data imports
// +schema:root=true
// +schema:group=tusker.sh
// +schema:version=v1
type DataMapper struct {
	// Kind is the resource type identifier
	Kind string `json:"kind" yaml:"kind" schema:"required,enum=DataMapper" description:"Resource type identifier"`

	// Version is the API version
	Version string `json:"version" yaml:"version" schema:"required,enum=v1" description:"API version"`

	// Metadata contains the mapping metadata
	Metadata Metadata `json:"metadata" yaml:"metadata" schema:"required" description:"Mapping metadata"`

	// Dataset is the source dataset identifier
	Dataset string `json:"dataset" yaml:"dataset" schema:"required,pattern=^[a-z0-9-_]+$,minLength=1,maxLength=50" description:"Dataset source identifier"`

	// DateFormat specifies the Go time format for parsing dates
	DateFormat string `json:"dateFormat,omitempty" yaml:"dateFormat,omitempty" schema:"default=2006-01-02T15:04:05Z" description:"Go time format for parsing date/datetime fields"`

	// FieldMappings defines the field mapping rules
	FieldMappings []FieldMapping `json:"fieldMappings" yaml:"fieldMappings" schema:"required,minItems=1" description:"Array of field mapping definitions"`
}

type Metadata struct {
	// Name is the human-readable name for the mapping
	Name string `json:"name" yaml:"name" schema:"required,minLength=1,maxLength=100" description:"Human-readable name for the mapping configuration"`

	// Description provides details about the mapping
	Description string `json:"description,omitempty" yaml:"description,omitempty" schema:"maxLength=500" description:"Description of the mapping configuration"`
}

type FieldMapping struct {
	// Source is the field name in the source dataset
	Source string `json:"source" yaml:"source" schema:"required,minLength=1,maxLength=100" description:"Source field name in the dataset"`

	// SourceType is the data type of the source field
	SourceType string `json:"sourceType,omitempty" yaml:"sourceType,omitempty" schema:"enum=string|int|float|bool|date|datetime,default=string" description:"Source field data type"`

	// Target is the field name in the target struct
	Target string `json:"target" yaml:"target" schema:"required,enum=ID|Title|Subtitle|Content|Author|Description|Language|URL|PublishedAt|CreatedAt|Metadata.SourceId|Metadata.SourceName|Metadata.Category|Metadata.ImportedAt" description:"Target field in the Article struct, or Metadata.Extra.<key> for a dataset-specific field"`

	// TargetType is the data type of the target field
	TargetType string `json:"targetType,omitempty" yaml:"targetType,omitempty" schema:"enum=string|int|float|bool|date|datetime|uuid|url|json,default=string" description:"Target field data type"`

	// Required indicates if this field mapping is mandatory
	Required bool `json:"required,omitempty" yaml:"required,omitempty" schema:"default=false" description:"Whether this field mapping is required"`
}

func (dm *DataMapper) Validate() error {
	if dm.Kind == "" {
		return fmt.Errorf("kind is required")
	}
	if dm.Version == "" {
		return fmt.Errorf("version is required")
	}
	if dm.Metadata.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if dm.Dataset == "" {
		return fmt.Errorf("dataset is required")
	}
	if len(dm.FieldMappings) == 0 {
		return fmt.Errorf("at least one field mapping is required")
	}
	for i, fm := range dm.FieldMappings {
		if fm.Source == "" {
			return fmt.Errorf("fieldMappings[%d] must have source defined", i)
		}
		if err := validateTarget(fm.Target); err != nil {
			return fmt.Errorf("fieldMappings[%d]: %w", i, err)
		}
	}
	return nil
}

// ExtraTargetPrefix marks a target that lands in Metadata.Extra rather than a
// schema field. Anything after the prefix is the key.
const ExtraTargetPrefix = "Metadata.Extra."

var articleTargets = map[string]struct{}{
	"ID": {}, "Title": {}, "Subtitle": {}, "Content": {}, "Author": {},
	"Description": {}, "Language": {}, "URL": {}, "PublishedAt": {},
	"CreatedAt": {}, "Metadata.SourceId": {}, "Metadata.SourceName": {},
	"Metadata.Category": {}, "Metadata.ImportedAt": {},
}

// renamedTargets maps retired target names to their replacement, so a stale
// mapping fails at load instead of silently diverting into Metadata.Extra.
var renamedTargets = map[string]string{
	"Metadata.PublishedAt": "PublishedAt",
}

func validateTarget(target string) error {
	if target == "" {
		return fmt.Errorf("must have target defined")
	}
	if renamed, ok := renamedTargets[target]; ok {
		return fmt.Errorf("target %q moved, use %q", target, renamed)
	}
	if _, ok := articleTargets[target]; ok {
		return nil
	}
	if key, ok := strings.CutPrefix(target, ExtraTargetPrefix); ok {
		if key == "" {
			return fmt.Errorf("target %q needs a key after %q", target, ExtraTargetPrefix)
		}
		return nil
	}
	return fmt.Errorf("unknown target %q", target)
}

type MappingError struct {
	Message string `json:"message" example:"missing source field: id"`
}

func (e *MappingError) Error() string {
	return fmt.Sprintf("datamapping error: %s", e.Message)
}
