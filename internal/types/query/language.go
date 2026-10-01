package query

import "fmt"

type Language string

const LanguageEnglish Language = "english"

var DefaultLanguage = LanguageEnglish

// Parse returns DefaultLanguage for an empty value and rejects every language
// but English: the corpus is stemmed in English, so a query stemmed in another
// language would match nothing rather than fail.
func (l Language) Parse() (Language, error) {
	switch l {
	case "":
		return DefaultLanguage, nil
	case LanguageEnglish:
		return l, nil
	default:
		return "", fmt.Errorf("language %q is not supported yet (only %s is)", string(l), LanguageEnglish)
	}
}
