package query

import (
	"errors"
	"slices"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/types/operator"
)

// Postgres shares weight band D between author and subtitle and has no typo
// tolerance; Elasticsearch honours every parameter.
var (
	postgresLike = Capabilities{
		Engine:          "postgres",
		DefaultOperator: operator.And,
		PhraseSlop:      true,
		Boosts:          true,
		Fields:          []Field{FieldTitle, FieldDescription, FieldContent},
	}
	elasticsearchLike = Capabilities{
		Engine:          "elasticsearch",
		DefaultOperator: operator.Or,
		Fuzziness:       true,
		PhraseSlop:      true,
		Boosts:          true,
		Fields:          SearchableFields(),
	}
)

func TestCheckCapabilities(t *testing.T) {
	tests := []struct {
		name        string
		caps        Capabilities
		params      Params
		handling    Handling
		wantErrFor  string
		wantIgnored []string
	}{
		{
			name:     "every parameter honoured",
			caps:     elasticsearchLike,
			params:   Params{Fields: []FieldWeight{{Field: FieldAuthor, Weight: 2}}, Fuzziness: FuzzinessAuto, Slop: 2},
			handling: HandlingStrict,
		},
		{
			name:       "strict rejects fuzziness the engine lacks",
			caps:       postgresLike,
			params:     Params{Fuzziness: FuzzinessAuto},
			handling:   HandlingStrict,
			wantErrFor: "fuzziness",
		},
		{
			name:        "lenient drops fuzziness the engine lacks",
			caps:        postgresLike,
			params:      Params{Fuzziness: Fuzziness1},
			handling:    HandlingLenient,
			wantIgnored: []string{"fuzziness"},
		},
		{
			name:     "no fuzziness is not a fuzziness request",
			caps:     postgresLike,
			params:   Params{Fuzziness: NoFuzziness},
			handling: HandlingStrict,
		},
		{
			name:     "an edit distance of zero is exact matching",
			caps:     postgresLike,
			params:   Params{Fuzziness: Fuzziness0},
			handling: HandlingStrict,
		},
		{
			name:       "strict rejects a field the engine cannot isolate",
			caps:       postgresLike,
			params:     Params{Fields: []FieldWeight{{Field: FieldTitle, Weight: 1}, {Field: FieldAuthor, Weight: 1}}},
			handling:   HandlingStrict,
			wantErrFor: "fields",
		},
		{
			name:        "lenient drops a field the engine cannot isolate",
			caps:        postgresLike,
			params:      Params{Fields: []FieldWeight{{Field: FieldSubtitle, Weight: 1}}},
			handling:    HandlingLenient,
			wantIgnored: []string{"fields"},
		},
		{
			name:     "a boost on a field the engine isolates is honoured",
			caps:     postgresLike,
			params:   Params{Fields: []FieldWeight{{Field: FieldTitle, Weight: 3}, {Field: FieldContent, Weight: 1}}},
			handling: HandlingStrict,
		},
		{
			name:        "lenient reports the field and its boost",
			caps:        postgresLike,
			params:      Params{Fields: []FieldWeight{{Field: FieldAuthor, Weight: 2}}},
			handling:    HandlingLenient,
			wantIgnored: []string{"boost", "fields"},
		},
		{
			name:        "a boost is ranking only, so strict drops it rather than reject",
			caps:        Capabilities{Engine: "plain", Fields: SearchableFields()},
			params:      Params{Fields: []FieldWeight{{Field: FieldTitle, Weight: 2}}},
			handling:    HandlingStrict,
			wantIgnored: []string{"boost"},
		},
		{
			name:       "strict rejects slop the engine lacks",
			caps:       Capabilities{Engine: "plain", Fields: SearchableFields()},
			params:     Params{Slop: 1},
			handling:   HandlingStrict,
			wantErrFor: "slop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ignored, err := CheckCapabilities(tt.caps, tt.params, tt.handling)
			if tt.wantErrFor != "" {
				var unsupported *UnsupportedParamError
				if !errors.As(err, &unsupported) {
					t.Fatalf("err = %v, want an UnsupportedParamError", err)
				}
				if unsupported.Param != tt.wantErrFor || unsupported.Engine != tt.caps.Engine {
					t.Errorf("unsupported %q on %q, want %q on %q", unsupported.Param, unsupported.Engine, tt.wantErrFor, tt.caps.Engine)
				}
				return
			}
			if err != nil {
				t.Fatalf("CheckCapabilities: %v", err)
			}
			var got []string
			for _, ig := range ignored {
				if ig.Engine != tt.caps.Engine {
					t.Errorf("ignored %q names engine %q, want %q", ig.Param, ig.Engine, tt.caps.Engine)
				}
				got = append(got, ig.Param)
			}
			slices.Sort(got)
			if !slices.Equal(got, tt.wantIgnored) {
				t.Errorf("ignored = %v, want %v", got, tt.wantIgnored)
			}
		})
	}
}

func TestParseHandling(t *testing.T) {
	tests := []struct {
		raw     string
		want    Handling
		wantErr bool
	}{
		{raw: "", want: HandlingStrict},
		{raw: "strict", want: HandlingStrict},
		{raw: "Lenient", want: HandlingLenient},
		{raw: "relaxed", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := ParseHandling(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseHandling(%q) = %q, want an error", tt.raw, got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("ParseHandling(%q) = %q, %v; want %q", tt.raw, got, err, tt.want)
			}
		})
	}
}
