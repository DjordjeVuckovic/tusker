package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DjordjeVuckovic/news-hunter/internal/ingest/reader"
	"github.com/DjordjeVuckovic/news-hunter/pkg/config/env"
	"github.com/spf13/cobra"
)

type preprocessConfig struct {
	InputPath   string
	OutputDir   string
	MappingPath string
	Workers     int
	WriteReport bool
}

type PreprocessReport struct {
	TotalRecords      int       `json:"total_records"`
	ProcessedRecords  int       `json:"processed_records"`
	DuplicatesRemoved int       `json:"duplicates_removed"`
	InvalidURLs       int       `json:"invalid_urls"`
	ProcessingTime    float64   `json:"processing_time_seconds"`
	OutputFile        string    `json:"output_file"`
	Timestamp         time.Time `json:"timestamp"`
}

func newPreprocessCmd() *cobra.Command {
	var cfg preprocessConfig
	cmd := &cobra.Command{
		Use:   "preprocess",
		Short: "Clean and map a raw dataset into a canonical JSONL file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			applyPreprocessEnvDefaults(&cfg)
			if cfg.InputPath == "" || cfg.OutputDir == "" || cfg.MappingPath == "" {
				return fmt.Errorf("--input, --output and --mapping are required")
			}
			return runPreprocess(cmd.Context(), cfg)
		},
	}

	f := cmd.Flags()
	f.StringVar(&cfg.InputPath, "input", "", "Path to the input CSV file")
	f.StringVar(&cfg.OutputDir, "output", "", "Output directory for canonical dataset")
	f.StringVar(&cfg.MappingPath, "mapping", "", "Path to the YAML field-mapping config")
	f.IntVar(&cfg.Workers, "workers", 16, "Number of parallel workers")
	f.BoolVar(&cfg.WriteReport, "report", false, "Write validation report")
	return cmd
}

// applyPreprocessEnvDefaults fills unset flags from the environment, preserving
// the original flag-default-from-env behaviour (INPUT_PATH/OUTPUT_PATH/MAPPING_CONFIG_PATH).
func applyPreprocessEnvDefaults(cfg *preprocessConfig) {
	if err := env.LoadDotEnv(os.Getenv("ENV"), "cmd/datapipe/preprocess.env"); err != nil {
		slog.Info("Skipping .env environment variables...", "error", err)
	}
	if cfg.InputPath == "" {
		cfg.InputPath = os.Getenv("INPUT_PATH")
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = os.Getenv("OUTPUT_PATH")
	}
	if cfg.MappingPath == "" {
		cfg.MappingPath = os.Getenv("MAPPING_CONFIG_PATH")
	}
}

func runPreprocess(ctx context.Context, cfg preprocessConfig) error {
	start := time.Now()

	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	inputBasename := strings.TrimSuffix(filepath.Base(cfg.InputPath), filepath.Ext(cfg.InputPath))
	outputFilename := fmt.Sprintf("%s_canonical.jsonl", inputBasename)
	outputPath := filepath.Join(cfg.OutputDir, outputFilename)

	mappingFile, err := os.Open(cfg.MappingPath)
	if err != nil {
		return fmt.Errorf("failed to open mapping config: %w", err)
	}
	defer mappingFile.Close()

	mappingCfg, err := reader.NewYAMLConfigLoader(mappingFile).Load(true)
	if err != nil {
		return fmt.Errorf("failed to load mapping config: %w", err)
	}
	mapper := reader.NewArticleMapper(mappingCfg)

	// Source field mapped to URL, used to detect invalid URLs that get blanked.
	urlSourceKey := ""
	for _, fm := range mappingCfg.FieldMappings {
		if fm.Target == "URL" {
			urlSourceKey = fm.Source
		}
	}

	dataFile, err := os.Open(cfg.InputPath)
	if err != nil {
		return fmt.Errorf("failed to open input file: %w", err)
	}
	defer dataFile.Close()

	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	report := &PreprocessReport{
		Timestamp:  time.Now(),
		OutputFile: outputFilename,
	}

	csvReader := reader.NewCSVReader(dataFile)
	resultsChan, err := csvReader.ReadParallel(ctx, cfg.Workers)
	if err != nil {
		return fmt.Errorf("failed to create parallel reader: %w", err)
	}

	encoder := json.NewEncoder(outFile)

	for result := range resultsChan {
		report.TotalRecords++

		if result.Err != nil {
			slog.Warn("failed to read record", "error", result.Err)
			continue
		}

		article, err := mapper.Map(result.Record)
		if err != nil {
			slog.Warn("failed to map record", "error", err)
			continue
		}

		if urlSourceKey != "" && result.Record[urlSourceKey] != "" && article.URL == "" {
			report.InvalidURLs++
		}

		if err := encoder.Encode(reader.ToCanonicalRecord(article)); err != nil {
			return fmt.Errorf("failed to write record: %w", err)
		}

		report.ProcessedRecords++
	}

	report.ProcessingTime = time.Since(start).Seconds()

	if cfg.WriteReport {
		if err := writeReport(cfg.OutputDir, inputBasename, report); err != nil {
			return fmt.Errorf("failed to write report: %w", err)
		}
	}

	logSummary(report)
	return nil
}

func writeReport(outputDir, basename string, report *PreprocessReport) error {
	reportPath := filepath.Join(outputDir, fmt.Sprintf("%s_report.json", basename))

	reportFile, err := os.Create(reportPath)
	if err != nil {
		return err
	}
	defer reportFile.Close()

	encoder := json.NewEncoder(reportFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return err
	}

	slog.Info("report written", "path", reportPath)
	return nil
}

func logSummary(report *PreprocessReport) {
	slog.Info("preprocessing summary",
		"total_records", report.TotalRecords,
		"processed_records", report.ProcessedRecords,
		"duplicates_removed", report.DuplicatesRemoved,
		"invalid_urls", report.InvalidURLs,
		"processing_time", fmt.Sprintf("%.2fs", report.ProcessingTime),
	)
}
