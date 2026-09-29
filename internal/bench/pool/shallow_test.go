package pool

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func entryWithSources(sources ...[]string) PoolEntry {
	e := PoolEntry{QueryID: "q"}
	for _, src := range sources {
		e.Docs = append(e.Docs, PooledDoc{DocID: uuid.New(), Sources: src})
	}
	return e
}

func docsFor(engine string, n int) [][]string {
	out := make([][]string, n)
	for i := range out {
		out[i] = []string{engine}
	}
	return out
}

func TestShallowEngines(t *testing.T) {
	tests := []struct {
		name  string
		pool  *PoolFile
		depth int
		want  []ShallowEngine
	}{
		{
			name:  "every engine filled the requested depth",
			pool:  &PoolFile{Queries: []PoolEntry{entryWithSources(append(docsFor("pg", 3), docsFor("es", 3)...)...)}},
			depth: 3,
		},
		{
			name:  "one engine never reached depth",
			pool:  &PoolFile{Queries: []PoolEntry{entryWithSources(append(docsFor("pg", 3), docsFor("tiger", 1)...)...)}},
			depth: 3,
			want:  []ShallowEngine{{Name: "tiger", MaxReturned: 1}},
		},
		{
			name: "the deepest query counts, not the narrowest",
			pool: &PoolFile{Queries: []PoolEntry{
				entryWithSources(docsFor("pg", 1)...),
				entryWithSources(docsFor("pg", 5)...),
			}},
			depth: 5,
		},
		{
			name: "a doc pooled from two engines counts for both",
			pool: &PoolFile{Queries: []PoolEntry{
				entryWithSources([]string{"pg", "es"}, []string{"pg", "es"}),
			}},
			depth: 2,
		},
		{
			name:  "reported in name order",
			pool:  &PoolFile{Queries: []PoolEntry{entryWithSources(append(docsFor("zeta", 1), docsFor("alpha", 1)...)...)}},
			depth: 4,
			want:  []ShallowEngine{{Name: "alpha", MaxReturned: 1}, {Name: "zeta", MaxReturned: 1}},
		},
		{
			name:  "no pool",
			depth: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ShallowEngines(tt.pool, tt.depth))
		})
	}
}
