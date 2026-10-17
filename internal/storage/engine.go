package storage

import "fmt"

// Engine names one search engine a corpus is loaded into.
type Engine string

const (
	PostgresNative Engine = "native"
	ParadeDB       Engine = "paradedb"
	Tiger          Engine = "tiger"
	Elasticsearch  Engine = "es"
)

var engines = []Engine{PostgresNative, ParadeDB, Tiger, Elasticsearch}

func ParseEngine(s string) (Engine, error) {
	for _, e := range engines {
		if string(e) == s {
			return e, nil
		}
	}
	return "", fmt.Errorf("unknown engine %q: want one of %q", s, engines)
}

// Type is the storage backend the engine runs on.
func (e Engine) Type() Type {
	switch e {
	case PostgresNative, ParadeDB, Tiger:
		return PG
	case Elasticsearch:
		return ES
	default:
		return ""
	}
}
