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
		outputDir       = flag.String("output", "api", "Output directory for the data mapping schema")
		strictMode      = flag.Bool("strict", true, "Enable strict validation mode for the data mapping schema")
		includeExamples = flag.Bool("examples", true, "Include the data mapping example configuration")
		baseURI         = flag.String("base-uri", "https://schemas.tusker.sh", "Base URI for the data mapping schema ID")
		verbose         = flag.Bool("verbose", false, "Enable verbose logging")
		dataPackageDir  = flag.String("datapackage-output", "", "Output directory for the data package descriptor schema; written only when set")
	)
	flag.Parse()

	if *verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}

	if *dataPackageDir != "" {
		dataPackageFile, err := writeDataPackageSchema(*dataPackageDir)
		if err != nil {
			log.Fatalf("Failed to generate data package schema: %v", err)
		}
		fmt.Printf("✅ Generated JSON schema: %s\n", dataPackageFile)
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
	return `# DataMapping example: maps the columns of a raw news dataset onto articles.

kind: DataMapper
version: v1
metadata:
  name: "Example News Dataset"
  description: "Field mapping for a raw news dataset"
dataset: "example"
dateFormat: "2006-01-02T15:04:05Z"
idStrategy:
  kind: uuidv5
  source: "url"
constants:
  Language: eng
output:
  format: parquet
fieldMappings:
  - source: "url"
    sourceType: "url"
    target: "URL"
    required: true
  - source: "title"
    target: "Title"
    required: true
  - source: "summary"
    target: "Description"
  - source: "content"
    target: "Content"
  - source: "published_date"
    sourceType: "datetime"
    target: "PublishedAt"
  - source: "publication"
    target: "Metadata.PublisherName"
  - source: "authors"
    target: "Metadata.Authors"
    separator: ";"
  - source: "section"
    target: "Metadata.Section"
  - source: "word_count"
    sourceType: "int"
    target: "Metadata.Extra.wordCount"
`
}
