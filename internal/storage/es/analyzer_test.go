package es

import (
	"context"
	"slices"
	"testing"

	pkgtesting "github.com/DjordjeVuckovic/tusker/pkg/testing"
	"github.com/jackc/pgx/v5"
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
	postgres := connectPostgres(t)

	probes := []struct {
		name string
		text string
	}{
		{
			name: "snowball stemming after the postgres stopword list",
			text: "about would should all because very after over news skies dying generously fairly",
		},
		{
			name: "function words postgres drops entirely",
			text: "the a an and or but if while is was been being have has had do does did",
		},
		{
			name: "contractions split at the apostrophe",
			text: "it's that’s don't doesn't they're company's",
		},
	}

	for _, probe := range probes {
		want := postgresEnglishLexemes(t, postgres, probe.text)
		for _, field := range []string{"title", "subtitle", "description", "content"} {
			t.Run(field+"/"+probe.name, func(t *testing.T) {
				res, err := client.Indices.Analyze().Index(cfg.IndexName).Field(field).Text(probe.text).Do(ctx)
				if err != nil {
					t.Fatalf("_analyze: %v", err)
				}
				var got []string
				for _, token := range res.Tokens {
					got = append(got, token.Token)
				}
				if !slices.Equal(got, want) {
					t.Errorf("tokens = %q, postgres lexemes = %q", got, want)
				}
			})
		}
	}
}

func connectPostgres(t *testing.T) *pgx.Conn {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, pkgtesting.NewPGContainerWithCleanup(ctx, t).ConnString)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// postgresEnglishLexemes returns the lexemes of to_tsvector('english', text) in
// position order, the order an analyzer emits its tokens.
func postgresEnglishLexemes(t *testing.T, conn *pgx.Conn, text string) []string {
	t.Helper()
	rows, err := conn.Query(context.Background(), `
		SELECT v.lexeme
		FROM unnest(to_tsvector('english', $1)) AS v, unnest(v.positions) AS position
		ORDER BY position`, text)
	if err != nil {
		t.Fatalf("to_tsvector %q: %v", text, err)
	}
	lexemes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("to_tsvector %q: %v", text, err)
	}
	return lexemes
}
