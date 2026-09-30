package embedding

import (
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/types/document"
)

func TestMapDocToPrompt_MatchesColabRecipe(t *testing.T) {
	tests := []struct {
		name    string
		article document.Article
		want    string
	}{
		{
			name:    "title and description joined by one space",
			article: document.Article{Title: "Title", Description: "Desc"},
			want:    "Title Desc",
		},
		{
			name:    "description missing",
			article: document.Article{Title: "Title"},
			want:    "Title",
		},
		{
			name:    "title missing",
			article: document.Article{Description: "Desc"},
			want:    "Desc",
		},
		{
			name:    "whitespace-only description dropped",
			article: document.Article{Title: "Title", Description: " \n\t"},
			want:    "Title",
		},
		{
			name:    "edges stripped, inner whitespace kept",
			article: document.Article{Title: " Title  A \n", Description: "\tDesc  B "},
			want:    "Title  A Desc  B",
		},
		{
			name:    "python whitespace separators stripped",
			article: document.Article{Title: "\x1cTitle\x1f", Description: "Desc"},
			want:    "Title Desc",
		},
		{
			name:    "content excluded",
			article: document.Article{Title: "Title", Description: "Desc", Content: "Body"},
			want:    "Title Desc",
		},
		{
			name:    "all empty",
			article: document.Article{Content: "Body"},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapDocToPrompt(tt.article); got != tt.want {
				t.Errorf("mapDocToPrompt() = %q, want %q", got, tt.want)
			}
		})
	}
}
