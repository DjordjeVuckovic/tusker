package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/DjordjeVuckovic/tusker/pkg/apis/datamapping"
	"github.com/DjordjeVuckovic/tusker/pkg/schema"
)

func main() {
	var (
		outputDir       = flag.String("output", "api", "Output directory for generated schemas")
		strictMode      = flag.Bool("strict", true, "Enable strict validation mode")
		includeExamples = flag.Bool("examples", true, "Include example configurations")
		baseURI         = flag.String("base-uri", "https://schemas.tusker.sh", "Base URI for schema IDs")
		verbose         = flag.Bool("verbose", false, "Enable verbose logging")
	)
	flag.Parse()

	if *verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}

	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		log.Fatalf("Failed to create output directory %s: %v", *outputDir, err)
	}

	config := schema.GeneratorConfig{
		IncludeExamples:     *includeExamples,
		StrictValidation:    *strictMode,
		UseDefinitions:      true,
		BaseURI:             *baseURI,
		SkipSchemaReference: false,
	}

	generator := schema.NewGeneratorWithConfig(config)

	if *verbose {
		log.Printf("Generating schemas with config: strict=%v, examples=%v, baseURI=%s",
			*strictMode, *includeExamples, *baseURI)
	}

	if *verbose {
		log.Printf("Generating JSON schema for DataMapping...")
	}

	schemaJSON, err := generator.GenerateJSONSchema(datamapping.DataMapper{})
	if err != nil {
		log.Fatalf("Failed to generate schema for DataMapping: %v", err)
	}

	jsonFile := filepath.Join(*outputDir, "datamapping-v1.json")
	if err := os.WriteFile(jsonFile, []byte(schemaJSON), 0644); err != nil {
		log.Fatalf("Failed to write JSON schema to %s: %v", jsonFile, err)
	}

	fmt.Printf("✅ Generated JSON schema: %s\n", jsonFile)

	if *includeExamples {
		if *verbose {
			log.Printf("Generating YAML example...")
		}

		yamlExample := generateYAMLExample()
		exampleDir := filepath.Join(*outputDir, "example")
		if err := os.MkdirAll(exampleDir, 0755); err != nil {
			log.Fatalf("Failed to create example directory %s: %v", exampleDir, err)
		}

		yamlFile := filepath.Join(exampleDir, "datamapping-example.yaml")
		if err := os.WriteFile(yamlFile, []byte(yamlExample), 0644); err != nil {
			log.Fatalf("Failed to write YAML example to %s: %v", yamlFile, err)
		}

		fmt.Printf("✅ Generated YAML example: %s\n", yamlFile)
	}

	if *verbose {
		log.Printf("Schema generation completed successfully")
	}
}

func generateYAMLExample() string {
	return `# DataMapping Example Configuration
# This file demonstrates the structure for defining field mappings

kind: DataMapper
version: v1
metadata:
  name: "Kaggle News Dataset"
  description: "Field mapping configuration for Kaggle news dataset import"
dataset: "kaggle"
dateFormat: "2006-01-02T15:04:05Z"
fieldMappings:
  - source: "title"
    sourceType: "string"
    target: "Title"
    targetType: "string"
    required: true
  - source: "content" 
    sourceType: "string"
    target: "Content"
    targetType: "string"
    required: true
  - source: "author"
    sourceType: "string" 
    target: "Author"
    targetType: "string"
    required: false
  - source: "published_date"
    sourceType: "datetime"
    target: "CreatedAt"
    targetType: "datetime"
    required: false
  - source: "language"
    sourceType: "string"
    target: "Language" 
    targetType: "string"
    required: false
`
}
