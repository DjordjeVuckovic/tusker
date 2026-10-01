package es

import (
	"context"
	"slices"
	"testing"

	pkgtesting "github.com/DjordjeVuckovic/tusker/pkg/testing"
)

func TestIndexer_AnalyzesTextFieldsLikePostgresEnglish(t *testing.T) {
	ctx := context.Background()
	container := pkgtesting.NewESContainer(ctx, t)
	cfg := ClientConfig{Addresses: []string{container.Address}, IndexName: "articles_analyzer_test"}

	if _, err := NewIndexer(ctx, cfg); err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	client, err := newClient(cfg)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	// Expected tokens are the lexemes of to_tsvector('english', text) on
	// PostgreSQL 18, in position order.
	probes := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "snowball stemming after the postgres stopword list",
			text: "about would should all because very after over news skies dying generously fairly",
			want: []string{"would", "news", "sky", "die", "generous", "fair"},
		},
		{
			name: "function words postgres drops entirely",
			text: "the a an and or but if while is was been being have has had do does did",
			want: nil,
		},
	}

	for _, field := range []string{"title", "subtitle", "description", "content"} {
		for _, probe := range probes {
			t.Run(field+"/"+probe.name, func(t *testing.T) {
				res, err := client.Indices.Analyze().Index(cfg.IndexName).Field(field).Text(probe.text).Do(ctx)
				if err != nil {
					t.Fatalf("_analyze: %v", err)
				}
				var got []string
				for _, token := range res.Tokens {
					got = append(got, token.Token)
				}
				if !slices.Equal(got, probe.want) {
					t.Errorf("tokens = %q, want %q", got, probe.want)
				}
			})
		}
	}
}
