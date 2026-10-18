package datapackage

import (
	"fmt"
	"io/fs"
)

// HashMismatchError reports a resource whose content no longer matches its hash.
type HashMismatchError struct {
	Resource string
	Want     string
	Got      string
}

func (e *HashMismatchError) Error() string {
	return fmt.Sprintf("resource %s: hash %s, descriptor declares %s", e.Resource, e.Got, e.Want)
}

// ByteCountMismatchError reports a resource of the wrong length; a longer one is read only to Want+1.
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

// NotFoundError reports a missing descriptor or resource file. It matches fs.ErrNotExist.
type NotFoundError struct {
	Location string
}

func (e *NotFoundError) Error() string { return e.Location + " does not exist" }

func (e *NotFoundError) Unwrap() error { return fs.ErrNotExist }
