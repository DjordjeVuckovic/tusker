package testing

import (
	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/google/uuid"
)

// SearchContractQuery is the query string whose recall over SearchContractCorpus
// is known by hand under the default search contract.
const SearchContractQuery = "budget tariff"

// SearchContractCorpus returns a corpus for SearchContractQuery and the IDs the
// default contract (title, description, content, AND) must return. The terms
// stem to themselves, so the expectation holds whether or not an engine stems.
func SearchContractCorpus() (articles []document.Article, want []uuid.UUID) {
	bothInTitle := uuid.MustParse("00000000-0000-0000-0000-00000000c001")
	titleAndContent := uuid.MustParse("00000000-0000-0000-0000-00000000c002")
	descriptionAndContent := uuid.MustParse("00000000-0000-0000-0000-00000000c003")
	oneTermOnlyInAuthor := uuid.MustParse("00000000-0000-0000-0000-00000000c004")
	bothOnlyInSubtitle := uuid.MustParse("00000000-0000-0000-0000-00000000c005")
	oneTermOnly := uuid.MustParse("00000000-0000-0000-0000-00000000c006")
	unrelated := uuid.MustParse("00000000-0000-0000-0000-00000000c007")

	articles = []document.Article{
		{ID: bothInTitle, Title: "budget tariff"},
		{ID: titleAndContent, Title: "budget", Content: "tariff"},
		{ID: descriptionAndContent, Title: "harbor", Description: "tariff", Content: "budget"},
		{ID: oneTermOnlyInAuthor, Title: "tariff", Author: "budget"},
		{ID: bothOnlyInSubtitle, Title: "harbor", Subtitle: "budget tariff"},
		{ID: oneTermOnly, Title: "budget"},
		{ID: unrelated, Title: "harbor"},
	}
	for i := range articles {
		articles[i].URL = "https://example.com/" + articles[i].ID.String()
		articles[i].Language = "english"
	}
	return articles, []uuid.UUID{bothInTitle, titleAndContent, descriptionAndContent}
}
