package judgment

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// LexicalStrategy is a deterministic baseline judge. It tokenizes the query
// description, then counts how many distinct query tokens appear in the
// document's title, description, and content.
type LexicalStrategy struct{}

func NewLexicalStrategy() *LexicalStrategy { return &LexicalStrategy{} }

func (LexicalStrategy) Name() string { return string(StrategyLexical) }

func (LexicalStrategy) Grade(_ context.Context, q GradingQuery, doc GradingDoc) (int, error) {
	terms := tokenize(q.Description)
	if len(terms) == 0 {
		return 0, fmt.Errorf("lexical strategy: query %q has no usable description (regenerate the pool or set --strategy manual)", q.ID)
	}

	titleTokens := tokenSet(doc.Title)
	descTokens := tokenSet(doc.Description)
	contentTokens := tokenSet(doc.Content)

	titleHits, otherHits := 0, 0
	for t := range terms {
		if titleTokens[t] {
			titleHits++
			continue
		}
		if descTokens[t] || contentTokens[t] {
			otherHits++
		}
	}

	total := len(terms)
	titleRatio := float64(titleHits) / float64(total)
	overallRatio := float64(titleHits+otherHits) / float64(total)

	switch {
	case titleRatio >= 0.5:
		return GradeHighly, nil
	case overallRatio >= 0.5:
		return GradeRelevant, nil
	case overallRatio >= 0.25:
		return GradeMarginally, nil
	default:
		return GradeNotRelev, nil
	}
}

var (
	tokenSplit = regexp.MustCompile(`[^\p{L}\p{N}]+`)
	stopwords  = map[string]struct{}{
		"a": {}, "an": {}, "and": {}, "the": {}, "or": {}, "of": {}, "to": {},
		"in": {}, "on": {}, "at": {}, "by": {}, "for": {}, "with": {}, "from": {},
		"is": {}, "are": {}, "was": {}, "were": {}, "be": {}, "been": {}, "being": {},
		"it": {}, "its": {}, "as": {}, "that": {}, "this": {}, "these": {}, "those": {},
		"news": {}, "article": {}, "search": {}, "query": {},
	}
)

func tokenize(s string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, raw := range tokenSplit.Split(strings.ToLower(s), -1) {
		if len(raw) < 3 {
			continue
		}
		if _, skip := stopwords[raw]; skip {
			continue
		}
		out[raw] = struct{}{}
	}
	return out
}

func tokenSet(s string) map[string]bool {
	out := make(map[string]bool)
	for _, raw := range tokenSplit.Split(strings.ToLower(s), -1) {
		if len(raw) < 3 {
			continue
		}
		out[raw] = true
	}
	return out
}
