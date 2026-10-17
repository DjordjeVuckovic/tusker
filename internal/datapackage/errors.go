package datapackage

import (
	"fmt"
	"io/fs"
	"strings"
)

// HashMismatchError reports a resource whose content no longer hashes to the
// descriptor's hash.
type HashMismatchError struct {
	Resource string
	Want     string
	Got      string
}

func (e *HashMismatchError) Error() string {
	return fmt.Sprintf("resource %s: hash %s, descriptor declares %s", e.Resource, e.Got, e.Want)
}

// ByteCountMismatchError reports a resource whose length differs from the
// descriptor's bytes. A longer resource is read only to Want+1, so Got is then
// a lower bound.
type ByteCountMismatchError struct {
	Resource string
	Want     int64
	Got      int64
}

func (e *ByteCountMismatchError) Error() string {
	if e.Got > e.Want {
		return fmt.Sprintf("resource %s: more than the %d bytes the descriptor declares", e.Resource, e.Want)
	}
	return fmt.Sprintf("resource %s: %d bytes, descriptor declares %d", e.Resource, e.Got, e.Want)
}

// ResourceNotFoundError reports a name the descriptor does not list.
type ResourceNotFoundError struct {
	Package string
	Name    string
}

func (e *ResourceNotFoundError) Error() string {
	return fmt.Sprintf("package %s has no resource %q", e.Package, e.Name)
}

// NotFoundError reports a descriptor or resource file absent at its location,
// whether a local path, an HTTP 404 or a missing S3 key. It matches
// fs.ErrNotExist.
type NotFoundError struct {
	Location string
}

func (e *NotFoundError) Error() string { return e.Location + " does not exist" }

func (e *NotFoundError) Unwrap() error { return fs.ErrNotExist }

// DescriptorConflictError reports a Write into a folder whose descriptor
// already records another version or other resource content, which at one
// version means the producer is not deterministic.
type DescriptorConflictError struct {
	Path        string
	Differences []string
}

func (e *DescriptorConflictError) Error() string {
	return fmt.Sprintf("%s describes different content: %s", e.Path, strings.Join(e.Differences, "; "))
}
