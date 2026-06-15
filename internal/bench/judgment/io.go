package judgment

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/DjordjeVuckovic/tusker/internal/bench/version"
	"gopkg.in/yaml.v3"
)

// WriteFile writes a JudgmentFile atomically: marshals to a tmp file in the
// same directory, fsyncs, then renames. Prevents readers from seeing a
// half-written YAML if the process is killed mid-write. Stamps schema_version
// every write so reloads always see a current artifact.
func WriteFile(jf *File, path string) error {
	jf.SchemaVersion = version.SchemaVersion
	data, err := yaml.Marshal(jf)
	if err != nil {
		return fmt.Errorf("marshal judgment file: %w", err)
	}
	return writeAtomic(path, data)
}

func ReadFile(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read judgment file: %w", err)
	}
	var jf File
	if err := yaml.Unmarshal(data, &jf); err != nil {
		return nil, fmt.Errorf("parse judgment file: %w", err)
	}
	if err := version.CheckSchema(jf.SchemaVersion, "annotations"); err != nil {
		return nil, err
	}
	return &jf, nil
}

// ReadFileIfExists is like ReadFile but returns (nil, nil) when the file is
// missing — useful for resume flows where the prior output may not exist yet.
func ReadFileIfExists(path string) (*File, error) {
	jf, err := ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return jf, err
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".bench-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp: %w", err)
	}
	return nil
}

// IncrementalWriter persists a JudgmentFile after every query so the user can
// resume after a crash, Ctrl-C, or rate-limit interrupt. Pass Append on the
// Runner's Sink and call Flush at the end to write the final file.
type IncrementalWriter struct {
	Path    string
	current File
}

func NewIncrementalWriter(path, strategy string) *IncrementalWriter {
	return &IncrementalWriter{
		Path: path,
		current: File{
			Strategy: strategy,
			Queries:  []Entry{},
		},
	}
}

// LoadPrior reads the existing output file (if any) into the writer's state.
// Returns the prior JudgmentFile so the caller can pass it as Runner.Existing.
func (w *IncrementalWriter) LoadPrior() (*File, error) {
	jf, err := ReadFileIfExists(w.Path)
	if err != nil {
		return nil, err
	}
	if jf == nil {
		return nil, nil
	}
	w.current = *jf
	return jf, nil
}

// Append adds (or updates) a query's judgments and flushes to disk atomically.
func (w *IncrementalWriter) Append(_ QueryProgress, entry Entry) error {
	for i, qe := range w.current.Queries {
		if qe.QueryID == entry.QueryID {
			w.current.Queries[i] = entry
			return w.flush()
		}
	}
	w.current.Queries = append(w.current.Queries, entry)
	return w.flush()
}

// Flush writes the current state to disk. Append already flushes; call this
// only if you mutated current directly.
func (w *IncrementalWriter) Flush() error { return w.flush() }

// Snapshot returns the in-memory JudgmentFile (read-only).
func (w *IncrementalWriter) Snapshot() *File { return &w.current }

func (w *IncrementalWriter) flush() error {
	return WriteFile(&w.current, w.Path)
}
