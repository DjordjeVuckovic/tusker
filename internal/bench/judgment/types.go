package judgment

import (
	"github.com/DjordjeVuckovic/tusker/internal/bench/meta"
	"github.com/google/uuid"
)

const (
	GradeUnjudged   = -1
	GradeNotRelev   = 0
	GradeMarginally = 1
	GradeRelevant   = 2
	GradeHighly     = 3
)

type GradedDoc struct {
	DocID uuid.UUID `yaml:"doc_id"`
	Grade int       `yaml:"grade"`
}

type File struct {
	SchemaVersion int       `yaml:"schema_version"`
	Meta          meta.Meta `yaml:"meta"`
	Strategy      string    `yaml:"strategy"`
	Queries       []Entry   `yaml:"queries"`
}

type Entry struct {
	QueryID string      `yaml:"query_id"`
	Docs    []GradedDoc `yaml:"docs"`
}
