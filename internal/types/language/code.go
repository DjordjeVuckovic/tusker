// Package language names the analysis language of a corpus and the text
// analysis each engine runs for it.
package language

import "fmt"

// Code is an ISO 639-3 language code.
type Code string

const English Code = "eng"

// Analysis names the text analysis each engine applies to one language.
type Analysis struct {
	PostgresConfig        string
	ParadeTokenizer       string
	TigerTextConfig       string
	ElasticsearchAnalyzer string
}

var analyses = map[Code]Analysis{
	English: {
		PostgresConfig:        "english",
		ParadeTokenizer:       "pdb.simple('stemmer=english', 'stopwords_language=english')",
		TigerTextConfig:       "english",
		ElasticsearchAnalyzer: "english_analyzer",
	},
}

func Parse(s string) (Code, error) {
	c := Code(s)
	if _, ok := analyses[c]; !ok {
		return "", fmt.Errorf("unsupported language %q: want an ISO 639-3 code, one of %q", s, English)
	}
	return c, nil
}

// Analysis returns the zero Analysis for a Code that Parse would reject.
func (c Code) Analysis() Analysis {
	return analyses[c]
}
