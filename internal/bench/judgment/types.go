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

// File is an annotations artifact. The grader lives in Meta.Judge — there is no
// top-level strategy field, so a judgment set has exactly one statement of who
// produced it.
type File struct {
	SchemaVersion int       `yaml:"schema_version"`
	Meta          meta.Meta `yaml:"meta"`
	Queries       []Entry   `yaml:"queries"`
}

// Judge returns the grader that produced this file, or the zero Judge when the
// artifact predates the judge block and carried no legacy fields either.
func (f *File) Judge() meta.Judge {
	if f.Meta.Judge == nil {
		return meta.Judge{}
	}
	return *f.Meta.Judge
}

type Entry struct {
	QueryID string      `yaml:"query_id"`
	Docs    []GradedDoc `yaml:"docs"`
}
