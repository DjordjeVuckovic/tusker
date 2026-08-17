package reader

import (
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// setFieldValue sets a reflect.Value field with the converted value
func setFieldValue(field reflect.Value, convertedValue interface{}) error {
	if !field.CanSet() {
		return fmt.Errorf("field cannot be set")
	}

	fieldValue := reflect.ValueOf(convertedValue)

	// Handle different field types
	switch field.Kind() {
	case reflect.String:
		if fieldValue.Kind() != reflect.String {
			return fmt.Errorf("field expects string but got %T", convertedValue)
		}
		field.SetString(fieldValue.String())
	case reflect.Int, reflect.Int64, reflect.Int32:
		if fieldValue.Kind() != reflect.Int64 {
			return fmt.Errorf("field expects integer but got %T", convertedValue)
		}
		field.SetInt(fieldValue.Int())
	case reflect.Float64, reflect.Float32:
		if fieldValue.Kind() != reflect.Float64 {
			return fmt.Errorf("field expects float but got %T", convertedValue)
		}
		field.SetFloat(fieldValue.Float())
	case reflect.Bool:
		if fieldValue.Kind() != reflect.Bool {
			return fmt.Errorf("field expects bool but got %T", convertedValue)
		}
		field.SetBool(fieldValue.Bool())
	default:
		// For complex types (time.Time, uuid.UUID, url.URL), use direct assignment
		if !fieldValue.Type().AssignableTo(field.Type()) {
			return fmt.Errorf("cannot assign %T to field of type %s", convertedValue, field.Type())
		}
		field.Set(fieldValue)
	}

	return nil
}

// SetFlatField converts a raw source value and assigns it to the named field.
func SetFlatField(obj reflect.Value, path string, value string, fieldType string, dateFormat string) error {
	convertedValue, err := convertValueToType(value, fieldType, dateFormat)
	if err != nil {
		return err
	}
	return AssignField(obj, path, convertedValue)
}

// SetNestedField converts a raw source value and assigns it to a field reached
// by walking a struct path.
func SetNestedField(obj reflect.Value, path []string, value string, fieldType string, dateFormat string) error {
	convertedValue, err := convertValueToType(value, fieldType, dateFormat)
	if err != nil {
		return err
	}
	return AssignNestedField(obj, path, convertedValue)
}

// AssignNestedField assigns an already-converted value to a field reached by
// walking a struct path.
func AssignNestedField(obj reflect.Value, path []string, convertedValue any) error {
	for i := 0; i < len(path)-1; i++ {
		obj = obj.FieldByName(path[i])
		if !obj.IsValid() {
			return fmt.Errorf("invalid field path: %s", path[i])
		}
		if obj.Kind() == reflect.Pointer {
			if obj.IsNil() {
				obj.Set(reflect.New(obj.Type().Elem()))
			}
			obj = obj.Elem()
		}
	}
	return AssignField(obj, path[len(path)-1], convertedValue)
}

// AssignField assigns an already-converted value to the named field.
func AssignField(obj reflect.Value, name string, convertedValue any) error {
	field := obj.FieldByName(name)

	if !field.IsValid() {
		return fmt.Errorf("invalid field path: %s", name)
	}
	if field.Kind() == reflect.Pointer {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
	}
	if !field.CanSet() {
		return fmt.Errorf("cannot set field %s", name)
	}

	return setFieldValue(field, convertedValue)
}

// parseDateTime tries multiple datetime formats to handle inconsistent data
func parseDateTime(value string, primaryFormat string) (time.Time, error) {
	// Try primary format first
	if t, err := time.Parse(primaryFormat, value); err == nil {
		return t, nil
	}

	// Common datetime formats to try as fallbacks
	fallbackFormats := []string{
		"2006-01-02 15:04:05.000000",  // with microseconds
		"2006-01-02 15:04:05",         // without microseconds
		"2006-01-02T15:04:05.000000Z", // ISO with microseconds
		"2006-01-02T15:04:05Z",        // ISO without microseconds
		"2006-01-02T15:04:05",         // ISO local
		time.RFC3339,                  // RFC3339
		time.RFC3339Nano,              // RFC3339 with nanoseconds
	}

	for _, format := range fallbackFormats {
		if format == primaryFormat {
			continue // Skip if already tried
		}
		if t, err := time.Parse(format, value); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse datetime value '%s' with any known format", value)
}

// convertValueToType converts a string value to the appropriate Go type based on fieldType
func convertValueToType(value string, fieldType string, dateFormat string) (interface{}, error) {
	switch fieldType {
	case "string":
		return value, nil
	case "int":
		intVal, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse int value '%s': %w", value, err)
		}
		return intVal, nil
	case "float":
		floatVal, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse float value '%s': %w", value, err)
		}
		return floatVal, nil
	case "bool":
		boolVal, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("failed to parse bool value '%s': %w", value, err)
		}
		return boolVal, nil
	case "date":
		t, err := time.Parse("2006-01-02", value)
		if err != nil {
			return nil, fmt.Errorf("failed to parse date value '%s': %w", value, err)
		}
		return t, nil
	case "datetime":
		t, err := parseDateTime(value, dateFormat)
		if err != nil {
			return nil, fmt.Errorf("failed to parse datetime value '%s': %w", value, err)
		}
		return t, nil
	case "uuid":
		id, err := uuid.Parse(value)
		if err != nil {
			return nil, fmt.Errorf("failed to parse uuid value '%s': %w", value, err)
		}
		return id, nil
	case "url":
		// Invalid URLs normalize to "" — the article is kept (URL is optional
		// provenance metadata), not dropped.
		normalized, _ := NormalizeURL(value)
		return normalized, nil
	default:
		return nil, fmt.Errorf("unsupported type: %s", fieldType)
	}
}
