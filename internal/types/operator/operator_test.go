package operator

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		input   string
		want    Operator
		wantErr bool
	}{
		{input: "", want: And},
		{input: "and", want: And},
		{input: "OR", want: Or},
		{input: "not", wantErr: true},
		{input: "xor", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := Parse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) = %q, want an error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("Parse(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
