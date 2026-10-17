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

// PublishOptions locates a package being published. StagingDir holds the
// package's resource file and must sit on the same filesystem as ParentDir,
// since publishing renames it.
type PublishOptions struct {
	StagingDir string
	ParentDir  string
}

// Published is a package in its version folder. Existing reports that the
// version was already published, so the staged copy was discarded.
type Published struct {
	Dir      string
	Package  Package
	Existing bool
}

// Publish versions a single-resource package (canonical, embeddings or query
// vectors) by its resource's bytes: it writes the descriptor in StagingDir with
// that version and renames StagingDir to ParentDir/<version>. A folder already
// at that version holds the same bytes, so Publish then removes StagingDir and
// returns the package already there. Raw packages keep their source version
// and use Write instead.
func Publish(ctx context.Context, pkg Package, opts PublishOptions) (Published, error) {
	if err := checkPublishable(pkg, opts); err != nil {
		return Published{}, err
	}
	written, err := write(ctx, opts.StagingDir, pkg, true)
	if err != nil {
		return Published{}, err
	}
	target := filepath.Join(opts.ParentDir, written.Version)
	if existing, ok, err := openPublished(ctx, target); err != nil || ok {
		if err != nil {
			return Published{}, err
		}
		return discardStaging(opts.StagingDir, existing)
	}
	if err := os.MkdirAll(opts.ParentDir, 0o755); err != nil {
		return Published{}, fmt.Errorf("create %s: %w", opts.ParentDir, err)
	}
	if err := os.Rename(opts.StagingDir, target); err != nil {
		// Another producer may have published the same bytes meanwhile.
		if existing, ok, openErr := openPublished(ctx, target); openErr == nil && ok {
			return discardStaging(opts.StagingDir, existing)
		}
		return Published{}, fmt.Errorf("publish %s as %s: %w", opts.StagingDir, target, err)
	}
	return Published{Dir: target, Package: written}, nil
}

func checkPublishable(pkg Package, opts PublishOptions) error {
	if len(pkg.Resources) != 1 {
		return fmt.Errorf("publish package %s: its version comes from one resource, it lists %d", pkg.Name, len(pkg.Resources))
	}
	if pkg.Version != "" {
		return fmt.Errorf("publish package %s: version %q is set, but Publish derives it from the resource", pkg.Name, pkg.Version)
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

func discardStaging(staging string, existing Published) (Published, error) {
	if err := os.RemoveAll(staging); err != nil {
		return Published{}, fmt.Errorf("remove staging %s: %w", staging, err)
	}
	return existing, nil
}

// versionOf names a package after the first 8 hex chars of its resource's
// sha256, so the version folder follows from the bytes.
func versionOf(r Resource) string {
	return strings.TrimPrefix(r.Hash, hashPrefix)[:8]
}
