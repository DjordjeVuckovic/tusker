package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func generatedDataPackageSchema(t *testing.T) map[string]any {
	t.Helper()
	raw, err := dataPackageSchema()
	if err != nil {
		t.Fatalf("dataPackageSchema: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode generated schema: %v", err)
	}
	return doc
}

func objectAt(t *testing.T, doc map[string]any, path ...string) map[string]any {
	t.Helper()
	node := doc
	for _, key := range path {
		next, ok := node[key].(map[string]any)
		if !ok {
			t.Fatalf("schema has no object at %v", path)
		}
		node = next
	}
	return node
}

func TestDataPackageSchemaAllowsOnlyListedProperties(t *testing.T) {
	doc := generatedDataPackageSchema(t)

	var closed func(path string, node map[string]any)
	closed = func(path string, node map[string]any) {
		if properties, ok := node["properties"].(map[string]any); ok {
			if node["additionalProperties"] != false {
				t.Errorf("%s: additionalProperties = %v, want false", path, node["additionalProperties"])
			}
			for name, child := range properties {
				childSchema, ok := child.(map[string]any)
				if !ok {
					t.Errorf("%s/%s: property schema is %T, want an object", path, name, child)
					continue
				}
				closed(path+"/"+name, childSchema)
			}
		}
		if items, ok := node["items"].(map[string]any); ok {
			closed(path+"/items", items)
		}
	}
	closed("", doc)

	for _, object := range []string{"mapping", "idStrategy", "producedBy", "report", "embedding", "canonical", "suite"} {
		if _, ok := objectAt(t, doc, "properties", object)["properties"]; !ok {
			t.Errorf("%s is not described as an object with properties", object)
		}
	}
}

func TestDataPackageSchemaDescribesDroppedAsCountsByReason(t *testing.T) {
	dropped := objectAt(t, generatedDataPackageSchema(t), "properties", "report", "properties", "dropped")

	values, ok := dropped["additionalProperties"].(map[string]any)
	if dropped["type"] != "object" || !ok || values["type"] != "integer" {
		t.Errorf("report.dropped = %v, want an object of integers", dropped)
	}
}

func TestCommittedDataPackageSchemaMatchesTheGenerator(t *testing.T) {
	committed, err := os.ReadFile(filepath.Join("..", "..", "api", "datapackage-spec", dataPackageSchemaFile))
	if err != nil {
		t.Fatalf("read committed schema: %v", err)
	}
	generated, err := dataPackageSchema()
	if err != nil {
		t.Fatalf("dataPackageSchema: %v", err)
	}
	if !bytes.Equal(committed, generated) {
		t.Errorf("api/datapackage-spec/%s is stale; regenerate it with `make schema-gen` and commit the result", dataPackageSchemaFile)
	}
}
