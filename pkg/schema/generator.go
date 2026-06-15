package schema

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// JSONSchema represents a JSON Schema document
type JSONSchema struct {
	Schema      string                 `json:"$schema,omitempty"`
	ID          string                 `json:"$id,omitempty"`
	Title       string                 `json:"title,omitempty"`
	Description string                 `json:"description,omitempty"`
	Type        string                 `json:"type,omitempty"`
	Format      string                 `json:"format,omitempty"`
	Required    []string               `json:"required,omitempty"`
	Properties  map[string]*JSONSchema `json:"properties,omitempty"`
	Items       *JSONSchema            `json:"items,omitempty"`
	Enum        []interface{}          `json:"enum,omitempty"`
	Default     interface{}            `json:"default,omitempty"`
	Pattern     string                 `json:"pattern,omitempty"`
	MinLength   *int                   `json:"minLength,omitempty"`
	MaxLength   *int                   `json:"maxLength,omitempty"`
	MinItems    *int                   `json:"minItems,omitempty"`
	MaxItems    *int                   `json:"maxItems,omitempty"`
	Minimum     *float64               `json:"minimum,omitempty"`
	Maximum     *float64               `json:"maximum,omitempty"`
	Examples    []interface{}          `json:"examples,omitempty"`
	Definitions map[string]*JSONSchema `json:"$defs,omitempty"`
	Ref         string                 `json:"$ref,omitempty"`
	AllOf       []*JSONSchema          `json:"allOf,omitempty"`
	AnyOf       []*JSONSchema          `json:"anyOf,omitempty"`
	OneOf       []*JSONSchema          `json:"oneOf,omitempty"`
	Not         *JSONSchema            `json:"not,omitempty"`
	If          *JSONSchema            `json:"if,omitempty"`
	Then        *JSONSchema            `json:"then,omitempty"`
	Else        *JSONSchema            `json:"else,omitempty"`
}

// ValidationError represents schema validation errors
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("schema validation error in field '%s': %s", e.Field, e.Message)
}

// GeneratorConfig holds configuration for schema generation
type GeneratorConfig struct {
	IncludeExamples     bool
	StrictValidation    bool
	UseDefinitions      bool
	BaseURI             string
	SkipSchemaReference bool
}

const schemaRef = "https://json-schema.org/draft/2020-12/schema"

// Generator generates JSON schemas from Go structs
type Generator struct {
	schemas    map[string]*JSONSchema
	config     GeneratorConfig
	validators map[string]func(string) error
}

// NewGenerator creates a new schema generator with default configuration
func NewGenerator() *Generator {
	return NewGeneratorWithConfig(GeneratorConfig{
		IncludeExamples:     true,
		StrictValidation:    true,
		UseDefinitions:      true,
		BaseURI:             "https://schemas.tusker.sh",
		SkipSchemaReference: false,
	})
}

// NewGeneratorWithConfig creates a new schema generator with custom configuration
func NewGeneratorWithConfig(config GeneratorConfig) *Generator {
	g := &Generator{
		schemas:    make(map[string]*JSONSchema),
		config:     config,
		validators: make(map[string]func(string) error),
	}
	g.setupDefaultValidators()
	return g
}

// setupDefaultValidators adds common validation functions
func (g *Generator) setupDefaultValidators() {
	g.validators["pattern"] = func(pattern string) error {
		_, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("invalid regex pattern: %w", err)
		}
		return nil
	}

	g.validators["format"] = func(format string) error {
		validFormats := []string{
			"date", "time", "date-time", "duration", "email", "hostname",
			"ipv4", "ipv6", "uri", "uri-reference", "uuid", "regex",
		}
		for _, valid := range validFormats {
			if format == valid {
				return nil
			}
		}
		return fmt.Errorf("invalid format '%s', must be one of: %v", format, validFormats)
	}
}

// GenerateSchema generates a JSON schema from a Go type
func (g *Generator) GenerateSchema(t reflect.Type) (*JSONSchema, error) {
	return g.generateSchemaForType(t, true)
}

func (g *Generator) generateSchemaForType(t reflect.Type, isRoot bool) (*JSONSchema, error) {
	// Handle pointers
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	schema := &JSONSchema{}

	// Only add schema reference to root elements unless configured otherwise
	if isRoot && !g.config.SkipSchemaReference {
		schema.Schema = schemaRef
	}

	switch t.Kind() {
	case reflect.Struct:
		return g.generateStructSchema(t, isRoot)
	case reflect.Slice:
		return g.generateSliceSchema(t)
	case reflect.String:
		schema.Type = "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		schema.Type = "integer"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		schema.Type = "integer"
		zero := float64(0)
		schema.Minimum = &zero
	case reflect.Float32, reflect.Float64:
		schema.Type = "number"
	case reflect.Bool:
		schema.Type = "boolean"
	case reflect.Interface:
		// For interface{} types, allow any type
		return &JSONSchema{}, nil
	default:
		return nil, fmt.Errorf("unsupported type: %s", t.Kind())
	}

	return schema, nil
}

func (g *Generator) generateStructSchema(t reflect.Type, isRoot bool) (*JSONSchema, error) {
	schema := &JSONSchema{
		Type:       "object",
		Properties: make(map[string]*JSONSchema),
	}

	if isRoot {
		schema.Schema = schemaRef

		// Extract schema metadata from type comments
		if comment := g.getTypeComment(t); comment != "" {
			schema.Description = comment
		}

		// Parse schema annotations from type comments
		g.parseSchemaAnnotations(t, schema)
	}

	var required []string

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}

		fieldName := g.getFieldName(field)
		if fieldName == "" {
			continue
		}

		fieldSchema, err := g.generateFieldSchema(field)
		if err != nil {
			return nil, fmt.Errorf("failed to generate schema for field %schemaRef: %w", field.Name, err)
		}

		schema.Properties[fieldName] = fieldSchema

		// Check if field is required
		if g.isFieldRequired(field) {
			required = append(required, fieldName)
		}
	}

	if len(required) > 0 {
		schema.Required = required
	}

	return schema, nil
}

func (g *Generator) generateSliceSchema(t reflect.Type) (*JSONSchema, error) {
	schema := &JSONSchema{
		Type: "array",
	}

	elemType := t.Elem()
	itemSchema, err := g.generateSchemaForType(elemType, false)
	if err != nil {
		return nil, fmt.Errorf("failed to generate schema for array items: %w", err)
	}

	schema.Items = itemSchema
	return schema, nil
}

func (g *Generator) generateFieldSchema(field reflect.StructField) (*JSONSchema, error) {
	fieldSchema, err := g.generateSchemaForType(field.Type, false)
	if err != nil {
		return nil, err
	}

	// Add description from field comment
	if desc := field.Tag.Get("description"); desc != "" {
		fieldSchema.Description = desc
	}

	// Parse schema tag
	if schemaTag := field.Tag.Get("schema"); schemaTag != "" {
		if err := g.parseSchemaTag(schemaTag, fieldSchema); err != nil {
			return nil, fmt.Errorf("invalid schema tag for field %s: %w", field.Name, err)
		}
	}

	return fieldSchema, nil
}

func (g *Generator) parseSchemaTag(tag string, schema *JSONSchema) error {
	parts := strings.Split(tag, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)

		if part == "required" {
			// Required is handled at the struct level
			continue
		}

		if strings.HasPrefix(part, "enum=") {
			enumStr := strings.TrimPrefix(part, "enum=")
			if enumStr == "" {
				if g.config.StrictValidation {
					return fmt.Errorf("enum value cannot be empty")
				}
				continue
			}
			enums := strings.Split(enumStr, "|")
			schema.Enum = make([]interface{}, len(enums))
			for i, e := range enums {
				schema.Enum[i] = strings.TrimSpace(e)
			}
		}

		if strings.HasPrefix(part, "default=") {
			defaultStr := strings.TrimPrefix(part, "default=")
			schema.Default = g.parseDefaultValue(defaultStr, schema.Type)
		}

		if strings.HasPrefix(part, "pattern=") {
			pattern := strings.TrimPrefix(part, "pattern=")
			if validator, exists := g.validators["pattern"]; exists {
				if err := validator(pattern); err != nil && g.config.StrictValidation {
					return fmt.Errorf("invalid pattern: %w", err)
				}
			}
			schema.Pattern = pattern
		}

		if strings.HasPrefix(part, "format=") {
			format := strings.TrimPrefix(part, "format=")
			if validator, exists := g.validators["format"]; exists {
				if err := validator(format); err != nil && g.config.StrictValidation {
					return fmt.Errorf("invalid format: %w", err)
				}
			}
			schema.Format = format
		}

		if strings.HasPrefix(part, "minLength=") {
			if val, err := strconv.Atoi(strings.TrimPrefix(part, "minLength=")); err == nil {
				if val < 0 && g.config.StrictValidation {
					return fmt.Errorf("minLength cannot be negative: %d", val)
				}
				schema.MinLength = &val
			} else if g.config.StrictValidation {
				return fmt.Errorf("invalid minLength value: %w", err)
			}
		}

		if strings.HasPrefix(part, "maxLength=") {
			if val, err := strconv.Atoi(strings.TrimPrefix(part, "maxLength=")); err == nil {
				if val < 0 && g.config.StrictValidation {
					return fmt.Errorf("maxLength cannot be negative: %d", val)
				}
				schema.MaxLength = &val
			} else if g.config.StrictValidation {
				return fmt.Errorf("invalid maxLength value: %w", err)
			}
		}

		if strings.HasPrefix(part, "minItems=") {
			if val, err := strconv.Atoi(strings.TrimPrefix(part, "minItems=")); err == nil {
				if val < 0 && g.config.StrictValidation {
					return fmt.Errorf("minItems cannot be negative: %d", val)
				}
				schema.MinItems = &val
			} else if g.config.StrictValidation {
				return fmt.Errorf("invalid minItems value: %w", err)
			}
		}

		if strings.HasPrefix(part, "maxItems=") {
			if val, err := strconv.Atoi(strings.TrimPrefix(part, "maxItems=")); err == nil {
				if val < 0 && g.config.StrictValidation {
					return fmt.Errorf("maxItems cannot be negative: %d", val)
				}
				schema.MaxItems = &val
			} else if g.config.StrictValidation {
				return fmt.Errorf("invalid maxItems value: %w", err)
			}
		}

		if strings.HasPrefix(part, "minimum=") {
			if val, err := strconv.ParseFloat(strings.TrimPrefix(part, "minimum="), 64); err == nil {
				schema.Minimum = &val
			} else if g.config.StrictValidation {
				return fmt.Errorf("invalid minimum value: %w", err)
			}
		}

		if strings.HasPrefix(part, "maximum=") {
			if val, err := strconv.ParseFloat(strings.TrimPrefix(part, "maximum="), 64); err == nil {
				schema.Maximum = &val
			} else if g.config.StrictValidation {
				return fmt.Errorf("invalid maximum value: %w", err)
			}
		}
	}

	// Validate constraints
	if err := g.validateSchemaConstraints(schema); err != nil && g.config.StrictValidation {
		return err
	}

	return nil
}

// parseDefaultValue converts string default to appropriate type
func (g *Generator) parseDefaultValue(value string, schemaType string) interface{} {
	switch schemaType {
	case "boolean":
		if val, err := strconv.ParseBool(value); err == nil {
			return val
		}
	case "integer":
		if val, err := strconv.ParseInt(value, 10, 64); err == nil {
			return val
		}
	case "number":
		if val, err := strconv.ParseFloat(value, 64); err == nil {
			return val
		}
	}
	return value
}

// validateSchemaConstraints validates schema constraints for consistency
func (g *Generator) validateSchemaConstraints(schema *JSONSchema) error {
	if schema.MinLength != nil && schema.MaxLength != nil {
		if *schema.MinLength > *schema.MaxLength {
			return fmt.Errorf("minLength (%d) cannot be greater than maxLength (%d)", *schema.MinLength, *schema.MaxLength)
		}
	}

	if schema.MinItems != nil && schema.MaxItems != nil {
		if *schema.MinItems > *schema.MaxItems {
			return fmt.Errorf("minItems (%d) cannot be greater than maxItems (%d)", *schema.MinItems, *schema.MaxItems)
		}
	}

	if schema.Minimum != nil && schema.Maximum != nil {
		if *schema.Minimum > *schema.Maximum {
			return fmt.Errorf("minimum (%f) cannot be greater than maximum (%f)", *schema.Minimum, *schema.Maximum)
		}
	}

	return nil
}

func (g *Generator) getFieldName(field reflect.StructField) string {
	jsonTag := field.Tag.Get("json")
	if jsonTag == "" {
		return strings.ToLower(field.Name[:1]) + field.Name[1:]
	}

	parts := strings.Split(jsonTag, ",")
	if parts[0] == "" {
		return strings.ToLower(field.Name[:1]) + field.Name[1:]
	}

	return parts[0]
}

func (g *Generator) isFieldRequired(field reflect.StructField) bool {
	schemaTag := field.Tag.Get("schema")
	return strings.Contains(schemaTag, "required")
}

func (g *Generator) getTypeComment(t reflect.Type) string {
	// This would typically come from parsing the source file
	// For now, return empty string
	return ""
}

func (g *Generator) parseSchemaAnnotations(t reflect.Type, schema *JSONSchema) {
	// Parse annotations like +schema:root=true, +schema:group=tusker.sh
	// In a real implementation, this would parse the source file comments
	// For now, we'll set some defaults
	schema.Title = t.Name()
	schema.ID = fmt.Sprintf("https://schemas.tusker.sh/%s", strings.ToLower(t.Name()))
}

// GenerateJSONSchema generates a JSON schema as a JSON string
func (g *Generator) GenerateJSONSchema(v interface{}) (string, error) {
	t := reflect.TypeOf(v)
	schema, err := g.GenerateSchema(t)
	if err != nil {
		return "", err
	}

	jsonBytes, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal schema to JSON: %w", err)
	}

	return string(jsonBytes), nil
}
