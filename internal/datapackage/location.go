package datapackage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// S3Config reaches an S3-compatible store such as R2. With an Endpoint set,
// requests use path-style addressing, which local S3 emulators need and R2
// accepts.
type S3Config struct {
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
}

func S3ConfigFromEnv() S3Config {
	return S3Config{
		Endpoint:  os.Getenv("DATASET_S3_ENDPOINT"),
		Region:    os.Getenv("DATASET_S3_REGION"),
		AccessKey: os.Getenv("DATASET_S3_ACCESS_KEY"),
		SecretKey: os.Getenv("DATASET_S3_SECRET_KEY"),
	}
}

func (opts Options) storeFor(ctx context.Context, location string) (store, string, error) {
	scheme, _, remote := strings.Cut(location, "://")
	if !remote {
		return newLocalStore(location)
	}
	u, err := url.Parse(location)
	if err != nil {
		return nil, "", fmt.Errorf("parse descriptor location %s: %w", location, err)
	}
	switch strings.ToLower(scheme) {
	case "https":
		return newHTTPSStore(u, opts.HTTPClient)
	case "s3":
		return newS3Store(ctx, u, opts.S3)
	case "file":
		return newLocalStore(u.Path)
	default:
		return nil, "", fmt.Errorf("descriptor location %s: unsupported scheme %q, want a path, https:// or s3://", location, scheme)
	}
}

// splitDescriptor separates a slash path into its directory and descriptor
// file name. A path ending in "/" names the directory, whose descriptor is
// datapackage.json.
func splitDescriptor(p string) (dir, name string) {
	i := strings.LastIndex(p, "/")
	dir, name = p[:i+1], p[i+1:]
	if name == "" {
		name = DescriptorName
	}
	return dir, name
}

type httpsStore struct {
	client *http.Client
	dir    *url.URL
}

func newHTTPSStore(u *url.URL, client *http.Client) (httpsStore, string, error) {
	if u.Host == "" {
		return httpsStore{}, "", fmt.Errorf("descriptor location %s names no host", u)
	}
	if client == nil {
		client = http.DefaultClient
	}
	dir, name := splitDescriptor(u.Path)
	base := *u
	base.Path, base.RawPath, base.RawQuery, base.Fragment = dir, "", "", ""
	return httpsStore{client: client, dir: &base}, name, nil
}

func (s httpsStore) location(rel string) string {
	return s.dir.ResolveReference(&url.URL{Path: rel}).String()
}

func (s httpsStore) open(ctx context.Context, rel string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.location(rel), nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", s.location(rel), err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", s.location(rel), err)
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		resp.Body.Close()
		return nil, &NotFoundError{Location: s.location(rel)}
	case resp.StatusCode != http.StatusOK:
		resp.Body.Close()
		return nil, fmt.Errorf("get %s: %s", s.location(rel), resp.Status)
	}
	return resp.Body, nil
}
