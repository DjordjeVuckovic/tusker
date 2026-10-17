package datapackage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Write hashes every resource under dir and writes the descriptor to
// dir/datapackage.json atomically, returning the descriptor as written. When
// dir already holds a descriptor with the same version and the same resource
// names, hashes and bytes, Write changes nothing and returns that descriptor,
// whatever its producedBy. Any other existing descriptor is never replaced and
// yields a *DescriptorConflictError.
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
	// Decoding the bytes, not returning pkg, makes the result equal what Open
	// reads back: omitempty turns empty maps and slices into nil.
	var written Package
	if err := json.Unmarshal(descriptor, &written); err != nil {
		return Package{}, fmt.Errorf("decode written descriptor: %w", err)
	}
	path := filepath.Join(dir, DescriptorName)
	existingBytes, err := os.ReadFile(path)
	switch {
	case err == nil:
		var existing Package
		if err := json.Unmarshal(existingBytes, &existing); err != nil {
			return Package{}, fmt.Errorf("decode existing descriptor %s: %w", path, err)
		}
		if differences := contentDifferences(existing, written); len(differences) > 0 {
			return Package{}, &DescriptorConflictError{Path: path, Differences: differences}
		}
		return existing, nil
	case !errors.Is(err, fs.ErrNotExist):
		return Package{}, fmt.Errorf("read existing descriptor: %w", err)
	}
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
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("rename descriptor into place: %w", err)
	}
	return nil
}

// contentDifferences lists what makes two descriptors describe different
// content: the version, or a resource's presence, hash or bytes. Everything
// else, producedBy included, may differ between reruns.
func contentDifferences(existing, written Package) []string {
	var differences []string
	if existing.Version != written.Version {
		differences = append(differences, fmt.Sprintf("version %q, not %q", existing.Version, written.Version))
	}
	byName := make(map[string]Resource, len(written.Resources))
	for _, r := range written.Resources {
		byName[r.Name] = r
	}
	for _, old := range existing.Resources {
		r, ok := byName[old.Name]
		switch {
		case !ok:
			differences = append(differences, fmt.Sprintf("resource %s is no longer written", old.Name))
		case r.Hash != old.Hash || r.Bytes != old.Bytes:
			differences = append(differences, fmt.Sprintf("resource %s is %s (%d bytes), not %s (%d bytes)",
				old.Name, old.Hash, old.Bytes, r.Hash, r.Bytes))
		}
		delete(byName, old.Name)
	}
	for _, r := range written.Resources {
		if _, added := byName[r.Name]; added {
			differences = append(differences, fmt.Sprintf("resource %s is new", r.Name))
		}
	}
	return differences
}

// contextReader lets a long copy stop between reads once ctx is done.
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
