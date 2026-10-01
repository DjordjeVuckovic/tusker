package query

import "testing"

func TestParseFieldBoost(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		want    FieldWeight
		wantErr bool
	}{
		{name: "unboosted field", spec: "content", want: FieldWeight{Field: FieldContent, Weight: 1}},
		{name: "boosted field", spec: "title^2.5", want: FieldWeight{Field: FieldTitle, Weight: 2.5}},
		{name: "fractional boost", spec: "description^0.25", want: FieldWeight{Field: FieldDescription, Weight: 0.25}},
		{name: "unknown field", spec: "body^2", wantErr: true},
		{name: "zero boost", spec: "title^0", wantErr: true},
		{name: "negative boost", spec: "title^-1", wantErr: true},
		{name: "non-number boost", spec: "title^high", wantErr: true},
		{name: "empty boost", spec: "title^", wantErr: true},
		{name: "NaN boost", spec: "title^NaN", wantErr: true},
		{name: "Inf boost", spec: "title^Inf", wantErr: true},
		{name: "-Inf boost", spec: "title^-Inf", wantErr: true},
		{name: "infinity boost", spec: "title^infinity", wantErr: true},
		{name: "two boosts", spec: "title^2^3", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFieldBoost(tt.spec)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseFieldBoost(%q) = %+v, want an error", tt.spec, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseFieldBoost(%q): %v", tt.spec, err)
			}
			if got != tt.want {
				t.Errorf("ParseFieldBoost(%q) = %+v, want %+v", tt.spec, got, tt.want)
			}
		})
	}
}

func TestNewMultiMatchQueryRejectsAnInvalidBoost(t *testing.T) {
	if _, err := NewMultiMatchQuery("climate", []string{"title^NaN", "content"}); err == nil {
		t.Fatal("NewMultiMatchQuery accepted a NaN boost")
	}
}
