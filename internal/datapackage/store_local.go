package datapackage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

type localStore struct{ dir string }

func newLocalStore(location string) (localStore, string, error) {
	info, err := os.Stat(location)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return localStore{}, "", &NotFoundError{Location: location}
	case err != nil:
		return localStore{}, "", fmt.Errorf("stat %s: %w", location, err)
	case info.IsDir():
		return localStore{dir: location}, DescriptorName, nil
	default:
		return localStore{dir: filepath.Dir(location)}, filepath.Base(location), nil
	}
}

func (s localStore) location(rel string) string {
	return filepath.Join(s.dir, filepath.FromSlash(rel))
}

func (s localStore) open(_ context.Context, rel string) (io.ReadCloser, error) {
	f, err := os.Open(s.location(rel))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, &NotFoundError{Location: s.location(rel)}
	}
	if err != nil {
		return nil, err
	}
	return f, nil
}
