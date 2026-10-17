package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/cli"
	"github.com/DjordjeVuckovic/tusker/internal/datapackage"
	"github.com/DjordjeVuckovic/tusker/internal/ingest"
	"github.com/DjordjeVuckovic/tusker/pkg/config/env"
	"github.com/spf13/cobra"
)

type preprocessConfig struct {
	// InputPath locates the raw package descriptor: a local path, https:// or s3://.
	InputPath string
	// OutputPath is the folder that receives <version>/, e.g. datasets/<ds>/canonical.
	OutputPath  string
	MappingPath string
	Packages    datapackage.Options
}

func newPreprocessCmd() *cobra.Command {
	var cfg preprocessConfig
	cmd := &cobra.Command{
		Use:   "preprocess",
		Short: "Map a raw data package into a canonical data package",
		RunE: func(cmd *cobra.Command, _ []string) error {
			applyPreprocessEnvDefaults(&cfg)
			if cfg.InputPath == "" || cfg.OutputPath == "" || cfg.MappingPath == "" {
				return errors.New("--input, --output and --mapping are required")
			}
			if err := validateFileExt(cfg.MappingPath, preprocessMappingExtensions); err != nil {
				return fmt.Errorf("invalid mapping file ext: %w", err)
			}
			cfg.Packages = datapackage.Options{S3: datapackage.S3ConfigFromEnv()}
			return runPreprocess(cmd.Context(), cmd.OutOrStdout(), cfg)
		},
	}

	f := cmd.Flags()
	f.StringVar(&cfg.InputPath, "input", "", "Raw data package: a datapackage.json or its folder, https:// or s3://")
	f.StringVar(&cfg.OutputPath, "output", "", "Folder that receives the canonical package as <version>/, e.g. datasets/<ds>/canonical")
	f.StringVar(&cfg.MappingPath, "mapping", "", "Path to the YAML field-mapping config")
	return cmd
}

var preprocessMappingExtensions = map[FileExt]struct{}{
	ExtYAML: {},
}

// applyPreprocessEnvDefaults fills unset flags from INPUT_PATH, OUTPUT_PATH and
// MAPPING_CONFIG_PATH.
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

func runPreprocess(ctx context.Context, out io.Writer, cfg preprocessConfig) error {
	start := time.Now()

	mapping, err := os.ReadFile(cfg.MappingPath)
	if err != nil {
		return fmt.Errorf("read mapping config: %w", err)
	}
	raw, err := datapackage.OpenWith(ctx, cfg.InputPath, cfg.Packages)
	if err != nil {
		return fmt.Errorf("open raw package: %w", err)
	}

	cli.Header(out, "tusker preprocess",
		cli.Field{Label: "raw", Value: raw.Location},
		cli.IntField("resources", len(raw.Package.Resources)),
		cli.Field{Label: "mapping", Value: cfg.MappingPath},
		cli.Field{Label: "output", Value: cfg.OutputPath},
	)

	result, err := ingest.Preprocess(ctx, ingest.PreprocessOptions{
		Raw:         raw,
		Mapping:     mapping,
		MappingPath: recordedMappingPath(cfg.MappingPath),
		OutputRoot:  cfg.OutputPath,
		ProducedBy:  preprocessProducer(),
	})
	if err != nil {
		return err
	}

	renderSummary(out, result, time.Since(start))
	return nil
}

// recordedMappingPath is the mapping path a committed descriptor can carry:
// relative to the working directory, or the bare file name when the mapping
// lives outside it.
func recordedMappingPath(path string) string {
	if !filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, path); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return filepath.Base(path)
}

func preprocessProducer() datapackage.ProducedBy {
	producer := datapackage.ProducedBy{Tool: "datapipe preprocess"}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return producer
	}
	producer.Version = info.Main.Version
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			producer.Commit = setting.Value
		}
	}
	return producer
}

func renderSummary(w io.Writer, result ingest.Preprocessed, elapsed time.Duration) {
	report := result.Package.Report
	var dropped int64
	for _, n := range report.Dropped {
		dropped += n
	}
	cli.Summary(w, "Preprocess complete",
		cli.Field{Label: "read", Value: strconv.FormatInt(report.Read, 10)},
		cli.Field{Label: "written", Value: strconv.FormatInt(report.Written, 10)},
		cli.Field{Label: "dropped", Value: strconv.FormatInt(dropped, 10)},
		cli.Field{Label: "deduped", Value: strconv.FormatInt(report.Deduped, 10)},
		cli.Field{Label: "version", Value: result.Package.Version},
		cli.Field{Label: "hash", Value: result.Package.Resources[0].Hash},
		cli.Field{Label: "package", Value: result.Dir},
		cli.Field{Label: "duration", Value: fmt.Sprintf("%.2fs", elapsed.Seconds())},
	)
	if dropped > 0 {
		cli.Warn(w, fmt.Sprintf("%d dropped: %s", dropped, dropBreakdown(report.Dropped)))
	}
}

// dropBreakdown renders drop reasons in a stable order, since map iteration
// is random.
func dropBreakdown(drops map[string]int64) string {
	reasons := make([]string, 0, len(drops))
	for reason := range drops {
		reasons = append(reasons, reason)
	}
	slices.Sort(reasons)

	parts := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		parts = append(parts, fmt.Sprintf("%d %s", drops[reason], reason))
	}
	return strings.Join(parts, ", ")
}
