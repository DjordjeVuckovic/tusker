package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCommittedMappingSchemaMatchesTheGenerator(t *testing.T) {
	committed, err := os.ReadFile(filepath.Join("..", "..", "api", "mapping-spec", mappingSchemaFile))
	if err != nil {
		t.Fatalf("read committed schema: %v", err)
	}
	generated, err := mappingSchema(true)
	if err != nil {
		t.Fatalf("mappingSchema: %v", err)
	}
	if !bytes.Equal(committed, generated) {
		t.Errorf("api/mapping-spec/%s is stale; regenerate it with `make schema-gen` and commit the result", mappingSchemaFile)
	}
}
