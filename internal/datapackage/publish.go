package datapackage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DefaultVersion names a package published without a version.
const DefaultVersion = "v1"

// PublishOptions locates a package being published. StagingDir holds the
// package's resource file and must sit on the same filesystem as ParentDir,
// since publishing renames it. Version names the folder the package lands in;
// empty means DefaultVersion.
type PublishOptions struct {
	StagingDir string
	ParentDir  string
	Version    string
}

// Published is a package in its version folder. Existing reports that the
// version already held the same bytes, so the staged copy was discarded.
type Published struct {
	Dir      string
	Package  Package
	Existing bool
}

// VersionConflictError reports a Publish into a version already published
// with other bytes. Nothing is replaced.
type VersionConflictError struct {
	Dir           string
	Version       string
	PublishedHash string
	StagedHash    string
}

func (e *VersionConflictError) Error() string {
	return fmt.Sprintf("version %s at %s already holds %s, not the staged %s; publish under another version",
		e.Version, e.Dir, e.PublishedHash, e.StagedHash)
}

// Publish writes the descriptor of a single-resource package (canonical or
// embeddings) in StagingDir and renames StagingDir to
// ParentDir/<version>. When that version is already published, Publish
// removes StagingDir and returns the published package if its resource has
// the same hash, or a *VersionConflictError if not. Raw and query-vectors
// packages live in fixed folders and use Write instead.
func Publish(ctx context.Context, pkg Package, opts PublishOptions) (Published, error) {
	if opts.Version == "" {
		opts.Version = DefaultVersion
	}
	if err := checkPublishable(pkg, opts); err != nil {
		return Published{}, err
	}
	pkg.Version = opts.Version
	staged, err := Write(ctx, opts.StagingDir, pkg)
	if err != nil {
		return Published{}, err
	}
	target := filepath.Join(opts.ParentDir, opts.Version)
	if existing, ok, err := openPublished(ctx, target); err != nil || ok {
		if err != nil {
			return Published{}, err
		}
		return keepPublished(opts.StagingDir, existing, staged)
	}
	if err := os.MkdirAll(opts.ParentDir, 0o755); err != nil {
		return Published{}, fmt.Errorf("create %s: %w", opts.ParentDir, err)
	}
	if err := os.Rename(opts.StagingDir, target); err != nil {
		// Another producer may have published this version meanwhile.
		if existing, ok, openErr := openPublished(ctx, target); openErr == nil && ok {
			return keepPublished(opts.StagingDir, existing, staged)
		}
		return Published{}, fmt.Errorf("publish %s as %s: %w", opts.StagingDir, target, err)
	}
	return Published{Dir: target, Package: staged}, nil
}

func checkPublishable(pkg Package, opts PublishOptions) error {
	if len(pkg.Resources) != 1 {
		return fmt.Errorf("publish package %s: it must list one resource, it lists %d", pkg.Name, len(pkg.Resources))
	}
	if pkg.Version != "" {
		return fmt.Errorf("publish package %s: version %q is set on the package; pass it in PublishOptions", pkg.Name, pkg.Version)
	}
	if err := validateResourcePath(opts.Version); err != nil || strings.Contains(opts.Version, "/") {
		return fmt.Errorf("publish package %s: version %q must be a single folder name", pkg.Name, opts.Version)
	}
	if opts.StagingDir == "" || opts.ParentDir == "" {
		return errors.New("publish needs both a staging and a parent folder")
	}
	staging, err := filepath.Abs(opts.StagingDir)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", opts.StagingDir, err)
	}
	parent, err := filepath.Abs(opts.ParentDir)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", opts.ParentDir, err)
	}
	if rel, err := filepath.Rel(staging, parent); err == nil && !strings.HasPrefix(rel, "..") {
		return fmt.Errorf("publish: parent %s lies inside staging %s, which is removed or moved", opts.ParentDir, opts.StagingDir)
	}
	return nil
}

func openPublished(ctx context.Context, dir string) (Published, bool, error) {
	if _, err := os.Stat(filepath.Join(dir, DescriptorName)); errors.Is(err, fs.ErrNotExist) {
		return Published{}, false, nil
	}
	opened, err := Open(ctx, dir)
	if err != nil {
		return Published{}, false, fmt.Errorf("open published %s: %w", dir, err)
	}
	return Published{Dir: dir, Package: opened.Package, Existing: true}, true, nil
}

// keepPublished discards the staged copy and returns the published package,
// unless the two resources differ.
func keepPublished(staging string, existing Published, staged Package) (Published, error) {
	if err := os.RemoveAll(staging); err != nil {
		return Published{}, fmt.Errorf("remove staging %s: %w", staging, err)
	}
	// Open has validated that the published package lists at least one resource.
	publishedHash := existing.Package.Resources[0].Hash
	if len(existing.Package.Resources) != 1 || publishedHash != staged.Resources[0].Hash {
		return Published{}, &VersionConflictError{
			Dir: existing.Dir, Version: staged.Version,
			PublishedHash: publishedHash, StagedHash: staged.Resources[0].Hash,
		}
	}
	return existing, nil
}
