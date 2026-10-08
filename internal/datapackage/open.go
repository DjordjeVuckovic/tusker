package datapackage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// maxDescriptorBytes bounds what Open reads, so a wrong location such as a
// data file cannot be pulled into memory whole.
const maxDescriptorBytes = 16 << 20

// Options configures OpenWith. The zero value opens local paths only plus
// https through http.DefaultClient.
type Options struct {
	HTTPClient *http.Client
	// TempDir holds remote resources while they are verified; empty means
	// os.TempDir.
	TempDir string
}

// Opened is a descriptor read from its location, ready to serve its
// resources.
type Opened struct {
	Package  Package
	Location string
	store    store
	tempDir  string
}

// store reads files relative to the descriptor's directory.
type store interface {
	open(ctx context.Context, rel string) (io.ReadCloser, error)
	location(rel string) string
}

// Open reads the descriptor at location: a local file or directory,
// https://…, or s3://bucket/key with credentials from the DATASET_S3_* env.
func Open(ctx context.Context, location string) (*Opened, error) {
	return OpenWith(ctx, location, Options{})
}

func OpenWith(ctx context.Context, location string, opts Options) (*Opened, error) {
	st, descriptorName, err := opts.storeFor(ctx, location)
	if err != nil {
		return nil, err
	}
	pkg, err := readDescriptor(ctx, st, descriptorName)
	if err != nil {
		return nil, err
	}
	return &Opened{Package: pkg, Location: st.location(descriptorName), store: st, tempDir: opts.TempDir}, nil
}

func readDescriptor(ctx context.Context, st store, name string) (Package, error) {
	body, err := st.open(ctx, name)
	if err != nil {
		return Package{}, err
	}
	defer body.Close()
	raw, err := io.ReadAll(io.LimitReader(body, maxDescriptorBytes+1))
	if err != nil {
		return Package{}, fmt.Errorf("read descriptor %s: %w", st.location(name), err)
	}
	if len(raw) > maxDescriptorBytes {
		return Package{}, fmt.Errorf("descriptor %s exceeds %d bytes", st.location(name), maxDescriptorBytes)
	}
	var pkg Package
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return Package{}, fmt.Errorf("decode descriptor %s: %w", st.location(name), err)
	}
	if err := pkg.validate(); err != nil {
		return Package{}, fmt.Errorf("descriptor %s: %w", st.location(name), err)
	}
	for _, r := range pkg.Resources {
		if err := validateDigest(r); err != nil {
			return Package{}, fmt.Errorf("descriptor %s: resource %s: %w", st.location(name), r.Name, err)
		}
	}
	return pkg, nil
}

func validateDigest(r Resource) error {
	hexDigest, ok := strings.CutPrefix(r.Hash, hashPrefix)
	if !ok {
		return fmt.Errorf("hash %q is not sha256", r.Hash)
	}
	if decoded, err := hex.DecodeString(hexDigest); err != nil || len(decoded) != 32 || strings.ToLower(hexDigest) != hexDigest {
		return fmt.Errorf("hash %q is not 64 lowercase hex chars", r.Hash)
	}
	if r.Bytes < 0 || (r.Bytes == 0 && r.Hash != emptyHash) {
		return fmt.Errorf("bytes %d does not fit hash %s", r.Bytes, r.Hash)
	}
	return nil
}

// emptyHash is the sha256 of zero bytes, the only hash a 0-byte resource has.
const emptyHash = hashPrefix + "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// Resource returns the named resource once its whole content has matched the
// descriptor's bytes and hash. A remote resource is downloaded to a temporary
// file first, which Close removes.
func (o *Opened) Resource(ctx context.Context, name string) (*ResourceFile, error) {
	r, ok := o.lookup(name)
	if !ok {
		return nil, &ResourceNotFoundError{Package: o.Package.Name, Name: name}
	}
	body, err := o.store.open(ctx, r.Path)
	if err != nil {
		return nil, err
	}
	if f, ok := body.(*os.File); ok {
		return verifyInPlace(ctx, r, f)
	}
	defer body.Close()
	return o.download(ctx, r, body)
}

func (o *Opened) lookup(name string) (Resource, bool) {
	for _, r := range o.Package.Resources {
		if r.Name == name {
			return r, true
		}
	}
	return Resource{}, false
}

func verifyInPlace(ctx context.Context, r Resource, f *os.File) (*ResourceFile, error) {
	if err := verify(ctx, r, f, io.Discard); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, fmt.Errorf("rewind resource %s: %w", r.Name, err)
	}
	return &ResourceFile{file: f, size: r.Bytes}, nil
}

func (o *Opened) download(ctx context.Context, r Resource, body io.Reader) (*ResourceFile, error) {
	tmp, err := os.CreateTemp(o.tempDir, "datapackage-"+r.Name+"-*")
	if err != nil {
		return nil, fmt.Errorf("create temp file for resource %s: %w", r.Name, err)
	}
	discard := func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}
	if err := verify(ctx, r, body, tmp); err != nil {
		discard()
		return nil, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		discard()
		return nil, fmt.Errorf("rewind resource %s: %w", r.Name, err)
	}
	return &ResourceFile{file: tmp, size: r.Bytes, temporary: true}, nil
}

// verify copies src to dst while hashing it and fails unless src holds exactly
// the declared bytes with the declared hash.
func verify(ctx context.Context, r Resource, src io.Reader, dst io.Writer) error {
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, h), contextReader{ctx: ctx, r: io.LimitReader(src, r.Bytes+1)})
	if err != nil {
		return fmt.Errorf("read resource %s: %w", r.Name, err)
	}
	if n != r.Bytes {
		return &ByteCountMismatchError{Resource: r.Name, Want: r.Bytes, Got: n}
	}
	if got := hashPrefix + hex.EncodeToString(h.Sum(nil)); got != r.Hash {
		return &HashMismatchError{Resource: r.Name, Want: r.Hash, Got: got}
	}
	return nil
}

// ResourceFile is a verified resource. It serves both sequential reads and
// the random access Parquet readers need.
type ResourceFile struct {
	file      *os.File
	size      int64
	temporary bool
}

func (f *ResourceFile) Read(p []byte) (int, error) { return f.file.Read(p) }

func (f *ResourceFile) ReadAt(p []byte, off int64) (int, error) { return f.file.ReadAt(p, off) }

func (f *ResourceFile) Size() int64 { return f.size }

// Close releases the file and removes it if it was a temporary download.
func (f *ResourceFile) Close() error {
	err := f.file.Close()
	if f.temporary {
		if removeErr := os.Remove(f.file.Name()); removeErr != nil && err == nil {
			err = removeErr
		}
	}
	return err
}
