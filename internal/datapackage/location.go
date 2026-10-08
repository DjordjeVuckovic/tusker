package datapackage

import (
	"context"
	"fmt"
	"strings"
)

func (opts Options) storeFor(_ context.Context, location string) (store, string, error) {
	if scheme, _, ok := strings.Cut(location, "://"); ok {
		return nil, "", fmt.Errorf("descriptor location %s: unsupported scheme %q", location, scheme)
	}
	return newLocalStore(location)
}
