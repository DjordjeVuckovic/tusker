package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/ingest"
	"github.com/DjordjeVuckovic/tusker/internal/ingest/reader"
	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/DjordjeVuckovic/tusker/pkg/config/env"
	"github.com/spf13/cobra"
)

const flushBatchSize = 1000

type preprocessConfig struct {
	InputPath   string
	OutputPath  string
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
		Short: "Clean and map a raw dataset into a canonical file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			applyPreprocessEnvDefaults(&cfg)
			if cfg.InputPath == "" || cfg.OutputPath == "" || cfg.MappingPath == "" {
				return fmt.Errorf("--input, --output and --mapping are required")
			}
			return runPreprocess(cmd.Context(), cfg)
		},
	}

	f := cmd.Flags()
	f.StringVar(&cfg.InputPath, "input", "", "Path to the input CSV file")
	f.StringVar(&cfg.OutputPath, "output", "", "Output file for canonical dataset")
	f.StringVar(&cfg.MappingPath, "mapping", "", "Path to the YAML field-mapping config")
	f.IntVar(&cfg.Workers, "workers", 16, "Number of parallel workers")
	f.BoolVar(&cfg.WriteReport, "report", false, "Write validation report")
	return cmd
}

// applyPreprocessEnvDefaults fills unset flags from the environment, preserving
// optionally load from env (INPUT_PATH/OUTPUT_PATH/MAPPING_CONFIG_PATH).
func applyPreprocessEnvDefaults(cfg *preprocessConfig) {
	if err := env.LoadDotEnv(os.Getenv("ENV"), "cmd/datapipe/preprocess.env"); err != nil {
		slog.Info("Skipping .env environment variables...", "error", err)
	}
	if cfg.InputPath == "" {
		cfg.InputPath = os.Getenv("INPUT_PATH")
	}
	if cfg.OutputPath == "" {
		cfg.OutputPath = os.Getenv("OUTPUT_PATH")
	}
	if cfg.MappingPath == "" {
		cfg.MappingPath = os.Getenv("MAPPING_CONFIG_PATH")
	}
}

func runPreprocess(ctx context.Context, cfg preprocessConfig) (err error) {
	start := time.Now()

	inputExt := filepath.Ext(cfg.InputPath)
	if inputExt != ".csv" {
		return fmt.Errorf("input file must be .csv")
	}
	inputBasename := strings.TrimSuffix(filepath.Base(cfg.InputPath), inputExt)

	outputPath := cfg.OutputPath
	outDir, outFilename := filepath.Split(cfg.OutputPath)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

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
		OutputFile: outFilename,
	}

	csvReader := reader.NewCSVReader(dataFile)
	resultsChan, err := csvReader.ReadParallel(ctx, cfg.Workers)
	if err != nil {
		return fmt.Errorf("failed to create parallel reader: %w", err)
	}

	writer, err := NewOutWriter(outFile, filepath.Ext(cfg.OutputPath))
	if err != nil {
		return err
	}
	defer func() {
		if cerr := writer.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close writer: %w", cerr)
		}
	}()

	arBuff := make([]document.CanonicalArticle, 0, flushBatchSize)
	flush := func() error {
		if len(arBuff) == 0 {
			return nil
		}
		if err := writer.Write(arBuff); err != nil {
			return err
		}
		arBuff = arBuff[:0]
		return nil
	}

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

		arBuff = append(arBuff, article.ToCanonical())
		report.ProcessedRecords++

		if len(arBuff) >= flushBatchSize {
			if err := flush(); err != nil {
				return fmt.Errorf("failed to write batch: %w", err)
			}
		}
	}
	if err := flush(); err != nil {
		return fmt.Errorf("failed to write final batch: %w", err)
	}

	report.ProcessingTime = time.Since(start).Seconds()

	if cfg.WriteReport {
		if err := writeReport(cfg.OutputPath, inputBasename, report); err != nil {
			return fmt.Errorf("failed to write report: %w", err)
		}
	}

	logSummary(report)
	return nil
}

func NewOutWriter(w io.Writer, ext string) (ingest.CanonicalWriter, error) {
	switch ext {
	case ".jsonl":
		return ingest.NewJsonlCanonicalWriter(w), nil
	case ".parquet":
		return ingest.NewParquetCanonicalWriter(w), nil
	default:
		return nil, fmt.Errorf("unknown output format: %s", ext)
	}
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
