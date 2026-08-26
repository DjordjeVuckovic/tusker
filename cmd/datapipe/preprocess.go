package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	CorpusId    string
	Workers     int
	WriteReport *bool
}

var preprocessInputExtensions = map[FileExt]struct{}{
	ExtCSV:     {},
	ExtJSONL:   {},
	ExtParquet: {},
}

var preprocessOutputExtensions = map[FileExt]struct{}{
	ExtJSONL:   {},
	ExtParquet: {},
}

var preprocessMappingExtensions = map[FileExt]struct{}{
	ExtYAML: {},
}

type PreprocessReport struct {
	CorpusId          string    `json:"corpus_id"`
	SHA256            string    `json:"sha256"`
	IdStrategy        string    `json:"id_strategy"`
	TotalRecords      int       `json:"total_records"`
	ProcessedRecords  int       `json:"processed_records"`
	DroppedRecords    int       `json:"dropped_records"`
	DuplicatesRemoved int       `json:"duplicates_removed"`
	InvalidURLs       int       `json:"invalid_urls"`
	ProcessingTime    float64   `json:"processing_time_seconds"`
	OutputFile        string    `json:"output_file"`
	Timestamp         time.Time `json:"timestamp"`

	// Drops counts rejects per "reason/field" so a mapping that empties a
	// dataset reads as a number, not as N identical warnings. Unreadable rows
	// count under "read_failed".
	Drops map[string]int `json:"drops,omitempty"`
}

func (r *PreprocessReport) recordDrop(err error) {
	r.DroppedRecords++
	if r.Drops == nil {
		r.Drops = make(map[string]int)
	}

	var drop *reader.MappingDropError
	if !errors.As(err, &drop) {
		r.Drops["read_failed"]++
		return
	}
	r.Drops[fmt.Sprintf("%s/%s", drop.Reason, drop.Target)]++
}

func newPreprocessCmd() *cobra.Command {
	var cfg preprocessConfig
	var writeReport bool
	cmd := &cobra.Command{
		Use:   "preprocess",
		Short: "Clean and map a raw dataset into a canonical file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Flags().Changed("report") {
				cfg.WriteReport = &writeReport
			}
			applyPreprocessEnvDefaults(&cfg)
			if cfg.InputPath == "" || cfg.OutputPath == "" || cfg.MappingPath == "" {
				return fmt.Errorf("--input, --output and --mapping are required")
			}
			if err := validateFileExt(cfg.InputPath, preprocessInputExtensions); err != nil {
				return fmt.Errorf("invalid input file ext: %w", err)
			}
			if err := validateFileExt(cfg.OutputPath, preprocessOutputExtensions); err != nil {
				return fmt.Errorf("invalid output file ext: %w", err)
			}
			if err := validateFileExt(cfg.MappingPath, preprocessMappingExtensions); err != nil {
				return fmt.Errorf("invalid mapping file ext: %w", err)
			}

			return runPreprocess(cmd.Context(), cfg)
		},
	}

	f := cmd.Flags()
	f.StringVar(&cfg.InputPath, "input", "", "Path to the input dataset file (.csv, .jsonl, .parquet)")
	f.StringVar(&cfg.OutputPath, "output", "", "Output file for canonical dataset")
	f.StringVar(&cfg.MappingPath, "mapping", "", "Path to the YAML field-mapping config")
	f.StringVar(&cfg.CorpusId, "corpus-id", "", "Corpus identifier recorded in the report (defaults to the mapping's dataset)")
	f.IntVar(&cfg.Workers, "workers", 16, "Number of parallel workers")
	f.BoolVar(&writeReport, "report", false, "Write validation report")
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
	if cfg.CorpusId == "" {
		cfg.CorpusId = os.Getenv("CORPUS_ID")
	}

	if cfg.WriteReport == nil {
		wr := os.Getenv("WRITE_REPORT") == "true"
		cfg.WriteReport = &wr
	}
}

func runPreprocess(ctx context.Context, cfg preprocessConfig) (err error) {
	start := time.Now()

	outDir, outFilename := filepath.Split(cfg.OutputPath)
	if outDir != "" {
		if err := os.MkdirAll(outDir, 0755); err != nil {
			return fmt.Errorf("failed to create output directory: %w", err)
		}
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
	mapper, err := reader.NewArticleMapper(mappingCfg)
	if err != nil {
		return fmt.Errorf("invalid mapping config: %w", err)
	}

	// Source field mapped to URL, used to detect invalid URLs that get blanked.
	urlSourceKey := ""
	for _, fm := range mappingCfg.FieldMappings {
		if fm.Target == "URL" {
			urlSourceKey = fm.Source
		}
	}

	report := &PreprocessReport{
		CorpusId:   cfg.CorpusId,
		Timestamp:  time.Now(),
		OutputFile: outFilename,
	}
	if report.CorpusId == "" {
		report.CorpusId = mappingCfg.Dataset
	}

	idKind, err := mappingCfg.IdStrategy.ResolvedKind()
	if err != nil {
		return err
	}
	report.IdStrategy = string(idKind)

	rawReader, dataset, err := OpenDatasetReader(cfg.InputPath)
	if err != nil {
		return err
	}
	defer dataset.Close()

	resultsChan, err := rawReader.ReadParallel(ctx, cfg.Workers)
	if err != nil {
		return fmt.Errorf("failed to create parallel reader: %w", err)
	}

	report.SHA256, err = writeCanonical(canonicalWriteOptions{
		OutputPath:   cfg.OutputPath,
		Mapper:       mapper,
		Records:      resultsChan,
		URLSourceKey: urlSourceKey,
		Report:       report,
	})
	if err != nil {
		return err
	}

	report.ProcessingTime = time.Since(start).Seconds()

	// A mistyped `source:` drops every record; without this the run would
	// succeed with an empty output file.
	if report.TotalRecords > 0 && report.ProcessedRecords == 0 {
		return fmt.Errorf("every one of %d records was dropped: %v", report.TotalRecords, report.Drops)
	}

	if cfg.WriteReport != nil && *cfg.WriteReport {
		if err := writeReport(
			outDir,
			strings.TrimSuffix(outFilename, filepath.Ext(outFilename)),
			report); err != nil {
			return fmt.Errorf("failed to write report: %w", err)
		}
	}

	logSummary(report)
	return nil
}

type canonicalWriteOptions struct {
	OutputPath   string
	Mapper       reader.Mapper
	Records      <-chan reader.ParallelReaderResult
	URLSourceKey string
	Report       *PreprocessReport
}

// writeCanonical streams mapped records to the canonical file and returns its
// SHA-256. The digest reads the bytes as they are written, and is only complete
// once the writer flushes, which for parquet is where the footer lands.
func writeCanonical(opts canonicalWriteOptions) (checksum string, err error) {
	outFile, err := os.Create(opts.OutputPath)
	if err != nil {
		return "", fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	digest := sha256.New()
	writer, err := NewCanonicalWriter(io.MultiWriter(outFile, digest), fileExt(opts.OutputPath))
	if err != nil {
		return "", err
	}
	defer func() {
		if writer == nil {
			return
		}
		if cerr := writer.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close writer: %w", cerr)
		}
	}()

	report := opts.Report
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

	for result := range opts.Records {
		report.TotalRecords++

		if result.Err != nil {
			slog.Warn("failed to read record", "error", result.Err)
			report.recordDrop(result.Err)
			continue
		}

		article, err := opts.Mapper.Map(result.Record)
		if err != nil {
			slog.Debug("dropped record", "error", err)
			report.recordDrop(err)
			continue
		}

		if opts.URLSourceKey != "" && result.Record[opts.URLSourceKey] != "" && article.URL == "" {
			report.InvalidURLs++
		}

		arBuff = append(arBuff, article.ToCanonical())
		report.ProcessedRecords++

		if len(arBuff) >= flushBatchSize {
			if err := flush(); err != nil {
				return "", fmt.Errorf("failed to write batch: %w", err)
			}
		}
	}
	if err := flush(); err != nil {
		return "", fmt.Errorf("failed to write final batch: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close writer: %w", err)
	}
	writer = nil

	return hex.EncodeToString(digest.Sum(nil)), nil
}

func writeReport(outDir, basename string, report *PreprocessReport) error {
	reportPath := filepath.Join(outDir, fmt.Sprintf("%s-report.json", basename))

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
		"corpus_id", report.CorpusId,
		"sha256", report.SHA256,
		"id_strategy", report.IdStrategy,
		"total_records", report.TotalRecords,
		"processed_records", report.ProcessedRecords,
		"dropped_records", report.DroppedRecords,
		"duplicates_removed", report.DuplicatesRemoved,
		"invalid_urls", report.InvalidURLs,
		"processing_time", fmt.Sprintf("%.2fs", report.ProcessingTime),
	)

	for reason, count := range report.Drops {
		slog.Warn("records dropped", "reason", reason, "count", count)
	}
}
