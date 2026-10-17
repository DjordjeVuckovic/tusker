package language

import "testing"

func TestParse_AcceptsOnlyISO6393English(t *testing.T) {
	tests := []struct {
		input   string
		want    Code
		wantErr bool
	}{
		{input: "eng", want: English},
		{input: "en", wantErr: true},
		{input: "english", wantErr: true},
		{input: "ENG", wantErr: true},
		{input: "srp", wantErr: true},
		{input: "", wantErr: true},
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

func TestEnglish_AnalysisNamesEachEngineChain(t *testing.T) {
	want := Analysis{
		PostgresConfig:        "english",
		ParadeTokenizer:       "pdb.simple('stemmer=english', 'stopwords_language=english')",
		TigerTextConfig:       "english",
		ElasticsearchAnalyzer: "english_analyzer",
	}

	if got := English.Analysis(); got != want {
		t.Errorf("English.Analysis() = %+v, want %+v", got, want)
	}
}
