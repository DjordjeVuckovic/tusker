package query

import "testing"

func TestParseFuzziness(t *testing.T) {
	tests := []struct {
		raw     string
		want    Fuzziness
		wantErr bool
	}{
		{raw: "", want: NoFuzziness},
		{raw: "none", want: NoFuzziness},
		{raw: "NONE", want: NoFuzziness},
		{raw: "auto", want: FuzzinessAuto},
		{raw: "AUTO", want: FuzzinessAuto},
		{raw: "0", want: Fuzziness0},
		{raw: "1", want: Fuzziness1},
		{raw: "2", want: Fuzziness2},
		{raw: "3", wantErr: true},
		{raw: "AUTO:3,6", wantErr: true},
		{raw: "fuzzy", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := ParseFuzziness(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseFuzziness(%q) = %q, want an error", tt.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseFuzziness(%q): %v", tt.raw, err)
			}
			if got != tt.want {
				t.Errorf("ParseFuzziness(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
