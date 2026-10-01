package pg

import (
	"context"
	"fmt"
	"slices"
	"testing"

	pkgtesting "github.com/DjordjeVuckovic/tusker/pkg/testing"
	"github.com/jackc/pgx/v5/pgxpool"
)

type paradeArticle struct {
	title       string
	description string
	content     string
	publishedAt string
	sourceName  string
}

var paradeCorpus = []paradeArticle{
	{title: "Clear sky over harbour", description: "weather", content: "calm evening", publishedAt: "2024-02-01", sourceName: "bbc.com"},
	{title: "Old habits die hard", description: "habits", content: "slow change", publishedAt: "2019-05-01", sourceName: "cnn.com"},
	{title: "A new season begins", description: "sport", content: "fresh start", publishedAt: "2024-03-01", sourceName: "bbc.com"},
	{title: "Evening news roundup", description: "bulletin", content: "daily summary", publishedAt: "2020-01-01", sourceName: "cnn.com"},
}

func newParadeDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	db := connectTo(t, pkgtesting.NewParadeDBContainerWithCleanup(ctx, t))

	for i, a := range paradeCorpus {
		_, err := db.Exec(ctx, `
			INSERT INTO articles (title, description, content, url, published_at, source_name)
			VALUES ($1, $2, $3, $4, $5::timestamptz, $6)`,
			a.title, a.description, a.content, fmt.Sprintf("https://example.com/%d", i), a.publishedAt, a.sourceName)
		if err != nil {
			t.Fatalf("seed article %q: %v", a.title, err)
		}
	}
	return db
}

func matchingTitles(t *testing.T, db *pgxpool.Pool, query string, args ...any) []string {
	t.Helper()
	rows, err := db.Query(context.Background(), query, args...)
	if err != nil {
		t.Fatalf("query %s: %v", query, err)
	}
	defer rows.Close()

	var titles []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			t.Fatalf("scan title: %v", err)
		}
		titles = append(titles, title)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("query %s: %v", query, err)
	}
	slices.Sort(titles)
	return titles
}

func TestParadeDBSearchIndex(t *testing.T) {
	db := newParadeDB(t)

	t.Run("title matches follow the postgres english stemmer", func(t *testing.T) {
		for _, word := range []string{"skies", "dying", "news"} {
			want := matchingTitles(t, db,
				`SELECT title FROM articles WHERE to_tsvector('english', title) @@ plainto_tsquery('english', $1)`, word)
			if len(want) == 0 {
				t.Fatalf("postgres matches no title for %q, so the comparison proves nothing", word)
			}
			got := matchingTitles(t, db,
				`SELECT title FROM articles WHERE id @@@ paradedb.match('title', $1)`, word)
			if !slices.Equal(got, want) {
				t.Errorf("%q: paradedb matches %q, postgres matches %q", word, got, want)
			}
		}
	})

	queryForms := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "parse",
			query: `SELECT title FROM articles WHERE id @@@ paradedb.parse('dying')`,
			want:  []string{"Old habits die hard"},
		},
		{
			name:  "quoted phrase",
			query: `SELECT title FROM articles WHERE id @@@ paradedb.parse('"clear skies"')`,
			want:  []string{"Clear sky over harbour"},
		},
		{
			name:  "quoted phrase with slop",
			query: `SELECT title FROM articles WHERE id @@@ paradedb.parse('"clear harbour"~2')`,
			want:  []string{"Clear sky over harbour"},
		},
		{
			name: "boolean with boost",
			query: `SELECT title FROM articles WHERE id @@@ paradedb.boolean(
				should => ARRAY[
					paradedb.boost(3.0, paradedb.match('title', 'skies')),
					paradedb.boost(2.0, paradedb.match('description', 'bulletins')),
					paradedb.match('content', 'starts')
				])`,
			want: []string{"A new season begins", "Clear sky over harbour", "Evening news roundup"},
		},
		{
			name:  "fuzzy match",
			query: `SELECT title FROM articles WHERE id @@@ paradedb.match('title', 'harbor', distance => 2)`,
			want:  []string{"Clear sky over harbour"},
		},
		{
			name: "published_at range inside the bm25 index",
			query: `SELECT title FROM articles
				WHERE id @@@ paradedb.boolean(must => ARRAY[
					paradedb.match('title', 'news season'),
					paradedb.range('published_at', tstzrange('2024-01-01', NULL))
				])`,
			want: []string{"A new season begins"},
		},
		{
			name: "published_at comparison next to a match",
			query: `SELECT title FROM articles
				WHERE id @@@ paradedb.match('title', 'news season') AND published_at < '2024-01-01'`,
			want: []string{"Evening news roundup"},
		},
		{
			name: "source name next to a match",
			query: `SELECT title FROM articles
				WHERE id @@@ paradedb.match('title', 'skies news') AND source_name = 'bbc.com'`,
			want: []string{"Clear sky over harbour"},
		},
	}
	for _, form := range queryForms {
		t.Run(form.name, func(t *testing.T) {
			if got := matchingTitles(t, db, form.query); !slices.Equal(got, form.want) {
				t.Errorf("titles = %q, want %q", got, form.want)
			}
		})
	}
}
