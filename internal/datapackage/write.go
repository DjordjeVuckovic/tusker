package datapackage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Write hashes the resources in dir and atomically writes dir/datapackage.json.
func Write(ctx context.Context, dir string, pkg Package) (Package, error) {
	if pkg.Name == "" {
		return Package{}, errors.New("package has no name")
	}
	if err := pkg.validate(); err != nil {
		return Package{}, err
	}
	pkg.Schema = Profile
	resources := make([]Resource, len(pkg.Resources))
	for i, r := range pkg.Resources {
		digest, err := digestFile(ctx, filepath.Join(dir, filepath.FromSlash(r.Path)))
		if err != nil {
			return Package{}, fmt.Errorf("hash resource %s: %w", r.Name, err)
		}
		r.Hash, r.Bytes = digest.hash, digest.bytes
		resources[i] = r
	}
	pkg.Resources = resources

	descriptor, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return Package{}, fmt.Errorf("encode descriptor: %w", err)
	}
	descriptor = append(descriptor, '\n')
	// Decoded so the result matches what Open reads: omitempty drops empty collections.
	var written Package
	if err := json.Unmarshal(descriptor, &written); err != nil {
		return Package{}, fmt.Errorf("decode written descriptor: %w", err)
	}
	path := filepath.Join(dir, DescriptorName)
	if err := writeAtomic(path, descriptor); err != nil {
		return Package{}, err
	}
	return written, nil
}

type digest struct {
	hash  string
	bytes int64
}

func digestFile(ctx context.Context, path string) (digest, error) {
	f, err := os.Open(path)
	if err != nil {
		return digest{}, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, contextReader{ctx: ctx, r: f})
	if err != nil {
		return digest{}, err
	}
	return digest{hash: hashPrefix + hex.EncodeToString(h.Sum(nil)), bytes: n}, nil
}

const hashPrefix = "sha256:"

func writeAtomic(path string, content []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("create temp descriptor: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync %s: %w", tmp.Name(), err)
	}
	// CreateTemp makes 0600 files.
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("rename descriptor into place: %w", err)
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
