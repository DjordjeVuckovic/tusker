package judgment

import (
	"fmt"

	"github.com/DjordjeVuckovic/tusker/internal/bench/meta"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
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

func (f *File) GradeCounts() (graded, unjudged int) {
	for _, qe := range f.Queries {
		for _, d := range qe.Docs {
			if d.Grade >= 0 {
				graded++
			} else {
				unjudged++
			}
		}
	}
	return graded, unjudged
}

type Entry struct {
	QueryID string      `yaml:"query_id"`
	Docs    []GradedDoc `yaml:"docs"`
}

// UnmarshalYAML rejects a doc without a grade key, which would otherwise
// decode as grade 0, a confident "not relevant".
func (e *Entry) UnmarshalYAML(value *yaml.Node) error {
	var raw struct {
		QueryID string `yaml:"query_id"`
		Docs    []struct {
			DocID uuid.UUID `yaml:"doc_id"`
			Grade *int      `yaml:"grade"`
		} `yaml:"docs"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	e.QueryID = raw.QueryID
	e.Docs = nil
	for _, d := range raw.Docs {
		if d.Grade == nil {
			return fmt.Errorf("query %q doc %s has no grade", raw.QueryID, d.DocID)
		}
		e.Docs = append(e.Docs, GradedDoc{DocID: d.DocID, Grade: *d.Grade})
	}
	return nil
}
