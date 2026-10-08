// Package dialect holds the syntax each engine type reads suite params in: how
// a block is checked before anything runs and what its executor receives.
package dialect

import (
	"fmt"

	"github.com/DjordjeVuckovic/tusker/internal/bench/engine"
	"github.com/DjordjeVuckovic/tusker/internal/bench/spec"
	"github.com/DjordjeVuckovic/tusker/internal/bench/suite"
)

// Dialect is how one engine type receives a suite query's params.
type Dialect interface {
	Name() string
	// Check rejects a block this dialect could not run as written.
	Check(block suite.Block) error
	// Request is what the executor receives for a resolved query of track.
	Request(track string, query *suite.ResolvedQuery) engine.Request
	// StoresTemplates reports whether the engine keeps suite templates and
	// renders them itself, so they are stored before any query runs.
	StoresTemplates() bool
}

var byEngineType = map[spec.EngineType]Dialect{
	spec.EnginePostgres:      Positional{},
	spec.EngineElasticsearch: Mustache{},
	spec.EngineAPI:           Literal{},
}

func For(engineType spec.EngineType) (Dialect, error) {
	d, ok := byEngineType[engineType]
	if !ok {
		return nil, fmt.Errorf("no query dialect for engine type %q", engineType)
	}
	return d, nil
}
