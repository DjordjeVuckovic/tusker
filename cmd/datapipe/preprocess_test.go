package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/datapackage"
	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/DjordjeVuckovic/tusker/internal/types/language"
	"github.com/parquet-go/parquet-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ccNewsLikeMapping = `kind: DataMapper
version: v1
metadata:
  name: Test news
dataset: test_news
idStrategy:
  kind: uuidv5
  source: url
dateFormat: "2006-01-02 15:04:05"
constants:
  Language: eng
output:
  format: parquet
fieldMappings:
  - {source: title, target: Title, required: true}
  - {source: text, target: Content}
  - {source: description, target: Description}
  - {source: url, sourceType: url, target: URL, required: true}
  - {source: date, sourceType: datetime, target: PublishedAt}
  - {source: authors, target: Metadata.Authors, separator: ";"}
  - {source: words, sourceType: int, target: Metadata.Extra.wordCount}
`

func mappingWith(t *testing.T, old, replacement string) string {
	t.Helper()
	require.Contains(t, ccNewsLikeMapping, old)
	return strings.Replace(ccNewsLikeMapping, old, replacement, 1)
}

// rawShardRow is a raw parquet shard in the shape the Hugging Face mirror
// lands: published column names, every value a string.
type rawShardRow struct {
	Title       string `parquet:"title"`
	Text        string `parquet:"text"`
	Description string `parquet:"description,optional"`
	URL         string `parquet:"url"`
	Date        string `parquet:"date,optional"`
	Authors     string `parquet:"authors,optional"`
	Words       string `parquet:"words,optional"`
}

type rawFile struct {
	name    string
	format  string
	content []byte
}

func csvFile(name, content string) rawFile {
	return rawFile{name: name, format: "csv", content: []byte(content)}
}

func jsonlFile(name, content string) rawFile {
	return rawFile{name: name, format: "jsonl", content: []byte(content)}
}

// parquetFile writes rows two to a row group, so a reader that walks row
// groups out of order shows up as a reordered corpus.
func parquetFile(t *testing.T, name string, rows ...rawShardRow) rawFile {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	require.NoError(t, err)
	w := parquet.NewGenericWriter[rawShardRow](f, parquet.MaxRowsPerRowGroup(2))
	_, err = w.Write(rows)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NoError(t, f.Close())
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return rawFile{name: name, format: "parquet", content: content}
}

const hubSource = "https://huggingface.co/datasets/example/news"

// writeRawPackage lays out a raw package the way the fetch scripts do and
// returns its folder.
func writeRawPackage(t *testing.T, dir string, files ...rawFile) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	pkg := datapackage.Package{
		Name: "test-news-raw",
		Sources: []datapackage.Source{
			{Title: "Hugging Face example/news", Path: hubSource, Version: "0123abcd"},
			{Title: "fetch notes", Path: "notes.txt"},
		},
	}
	for _, f := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, f.name), f.content, 0o644))
		pkg.Resources = append(pkg.Resources, datapackage.Resource{Name: f.name, Path: f.name, Format: f.format})
	}
	_, err := datapackage.Write(context.Background(), dir, pkg)
	require.NoError(t, err)
}

type preprocessRun struct {
	rawDir      string
	mappingPath string
	outputRoot  string
}

func newPreprocessRun(t *testing.T, mapping string, files ...rawFile) preprocessRun {
	t.Helper()
	dir := t.TempDir()
	run := preprocessRun{
		rawDir:      filepath.Join(dir, "raw"),
		mappingPath: filepath.Join(dir, "mapping.yaml"),
		outputRoot:  filepath.Join(dir, "canonical"),
	}
	require.NoError(t, os.WriteFile(run.mappingPath, []byte(mapping), 0o644))
	writeRawPackage(t, run.rawDir, files...)
	return run
}

func (r preprocessRun) execute(ctx context.Context) error {
	cmd := newPreprocessCmd()
	cmd.SetArgs([]string{"--input", r.rawDir, "--mapping", r.mappingPath, "--output", r.outputRoot})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd.ExecuteContext(ctx)
}

// packageDir is the one version folder preprocess wrote under the output root.
func (r preprocessRun) packageDir(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir(r.outputRoot)
	require.NoError(t, err)
	require.Len(t, entries, 1, "the output root holds exactly the package folder")
	return filepath.Join(r.outputRoot, entries[0].Name())
}

func sha256Of(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// canonicalParquetRow reads the canonical parquet columns by the names the
// contract gives them, independently of the writer.
type canonicalParquetRow struct {
	ID              string `parquet:"id"`
	URL             string `parquet:"url"`
	Title           string `parquet:"title"`
	Description     string `parquet:"description,optional"`
	Content         string `parquet:"content"`
	PublishedAt     string `parquet:"publishedAt,optional"`
	Language        string `parquet:"language"`
	PublisherDomain string `parquet:"publisherDomain,optional"`
	Metadata        string `parquet:"metadata,optional"`
}

type canonicalRows struct {
	urls  []string
	byURL map[string]document.Article
}

// readArticles opens the canonical package as a loader would, verifying the
// resource hash, and decodes every row.
func readArticles(t *testing.T, dir string) (datapackage.Package, canonicalRows) {
	t.Helper()
	ctx := context.Background()
	opened, err := datapackage.Open(ctx, dir)
	require.NoError(t, err)
	file, err := opened.Resource(ctx, datapackage.ArticlesResource)
	require.NoError(t, err)
	defer file.Close()

	rows := canonicalRows{byURL: map[string]document.Article{}}
	add := func(a document.Article) {
		rows.urls = append(rows.urls, a.URL)
		rows.byURL[a.URL] = a
	}
	switch format := opened.Package.Resources[0].Format; format {
	case "jsonl":
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			var a document.Article
			require.NoError(t, json.Unmarshal(scanner.Bytes(), &a))
			add(a)
		}
		require.NoError(t, scanner.Err())
	case "parquet":
		r := parquet.NewGenericReader[canonicalParquetRow](file)
		defer r.Close()
		buf := make([]canonicalParquetRow, r.NumRows())
		n, err := r.Read(buf)
		if !errors.Is(err, io.EOF) {
			require.NoError(t, err)
		}
		for _, row := range buf[:n] {
			add(articleFromParquet(t, row))
		}
	default:
		t.Fatalf("unexpected format %q", format)
	}
	return opened.Package, rows
}

func articleFromParquet(t *testing.T, row canonicalParquetRow) document.Article {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"id": row.ID, "url": row.URL, "title": row.Title, "description": row.Description,
		"content": row.Content, "language": row.Language, "publisherDomain": row.PublisherDomain,
		"metadata": json.RawMessage(row.Metadata),
	})
	require.NoError(t, err)
	var a document.Article
	require.NoError(t, json.Unmarshal(encoded, &a))
	if row.PublishedAt != "" {
		a.PublishedAt, err = time.Parse(time.RFC3339, row.PublishedAt)
		require.NoError(t, err)
	}
	return a
}

func TestPreprocess_WritesACanonicalPackageInSourceOrder(t *testing.T) {
	run := newPreprocessRun(t, ccNewsLikeMapping,
		parquetFile(t, "train-0.parquet",
			rawShardRow{Title: "Storm hits coast", Text: "Body one", Description: "Lead one", URL: "https://example.com/a/1", Date: "2019-03-04 05:06:07", Authors: "Jane Doe; John Roe", Words: "412"},
			rawShardRow{Title: "Markets fall", Text: "Body two", URL: "https://example.com/a/2"},
			rawShardRow{Title: "Rain returns", Text: "Body three", URL: "https://example.com/a/3"},
		),
		csvFile("train-1.csv", "title,text,url\nLate edition,Body four,https://example.com/b/4\nNight news,Body five,https://example.com/b/5\n"),
	)

	require.NoError(t, run.execute(t.Context()))

	pkg, rows := readArticles(t, run.packageDir(t))
	assert.Equal(t, []string{
		"https://example.com/a/1", "https://example.com/a/2", "https://example.com/a/3",
		"https://example.com/b/4", "https://example.com/b/5",
	}, rows.urls)

	first := rows.byURL["https://example.com/a/1"]
	assert.Equal(t, "Storm hits coast", first.Title)
	assert.Equal(t, "Lead one", first.Description)
	assert.Equal(t, "Body one", first.Content)
	assert.Equal(t, time.Date(2019, 3, 4, 5, 6, 7, 0, time.UTC), first.PublishedAt.UTC())
	assert.Equal(t, language.English, first.Language)
	assert.Equal(t, []string{"Jane Doe", "John Roe"}, first.Metadata.Authors)
	assert.Equal(t, map[string]any{"wordCount": float64(412)}, first.Metadata.Extra)
	assert.Equal(t, language.English, rows.byURL["https://example.com/b/5"].Language)

	assert.Equal(t, "test-news-canonical", pkg.Name)
	assert.Equal(t, "article/v2", pkg.ArticleSchema)
	assert.Equal(t, language.English, pkg.Language)
	resource := pkg.Resources[0]
	assert.Equal(t, "articles", resource.Name)
	assert.Equal(t, "articles.parquet", resource.Path)
	assert.Equal(t, "parquet", resource.Format)
	assert.Equal(t, sha256Of(t, filepath.Join(run.packageDir(t), "articles.parquet")), resource.Hash)
	assert.Equal(t, &datapackage.Report{Read: 5, Written: 5}, pkg.Report)
	assert.Equal(t, []datapackage.Source{{Title: "Hugging Face example/news", Path: hubSource, Version: "0123abcd"}}, pkg.Sources)
	assert.Equal(t, "datapipe preprocess", pkg.ProducedBy.Tool)
}

func TestPreprocess_DescriptorAloneRecomputesItsVersion(t *testing.T) {
	run := newPreprocessRun(t, ccNewsLikeMapping,
		csvFile("b.csv", "title,text,url\nOne,Body,https://example.com/1\n"),
		csvFile("a.csv", "title,text,url\nTwo,Body,https://example.com/2\n"),
	)

	require.NoError(t, run.execute(t.Context()))

	pkg, _ := readArticles(t, run.packageDir(t))
	rawB, rawA := sha256Of(t, filepath.Join(run.rawDir, "b.csv")), sha256Of(t, filepath.Join(run.rawDir, "a.csv"))
	assert.Equal(t, &datapackage.RawRef{
		Name:      "test-news-raw",
		Resources: []datapackage.ResourceRef{{Name: "b.csv", Hash: rawB}, {Name: "a.csv", Hash: rawA}},
	}, pkg.Raw)
	assert.Equal(t, sha256Of(t, run.mappingPath), pkg.Mapping.Hash)
	assert.Equal(t, filepath.Base(run.packageDir(t)), pkg.Version)

	inputs := []string{pkg.ArticleSchema, pkg.Mapping.Hash}
	for _, r := range pkg.Raw.Resources {
		inputs = append(inputs, r.Hash)
	}
	assert.Equal(t, pkg.Version, datapackage.Version("article-v2", inputs...))
	assert.Equal(t, datapackage.Version("article-v2", "article/v2", sha256Of(t, run.mappingPath), rawB, rawA), pkg.Version)
}

func TestPreprocess_CountsEveryDroppedRecordByReason(t *testing.T) {
	run := newPreprocessRun(t, ccNewsLikeMapping,
		csvFile("part.csv", "title,text,url\n"+
			"Kept,Body,https://example.com/kept\n"+
			"   ,Blank title,https://example.com/blank-title\n"+
			"No url,Body,\n"+
			"Bad url,Body,not a url\n"+
			"Short row,Body\n"),
	)

	require.NoError(t, run.execute(t.Context()))

	pkg, rows := readArticles(t, run.packageDir(t))
	assert.Equal(t, []string{"https://example.com/kept"}, rows.urls)
	assert.Equal(t, &datapackage.Report{
		Read:    5,
		Written: 1,
		Dropped: map[string]int64{
			"empty_required_value/Title": 1,
			"empty_required_value/URL":   1,
			"invalid_url/URL":            1,
			"malformed_record":           1,
		},
	}, pkg.Report)
}

func TestPreprocess_WritesJSONLArticlesWhenTheMappingAsksForIt(t *testing.T) {
	run := newPreprocessRun(t, mappingWith(t, "format: parquet", "format: jsonl"),
		jsonlFile("part.jsonl", `{"title":"One","text":"Body","url":"https://example.com/1","authors":"A;B"}`+"\n"),
	)

	require.NoError(t, run.execute(t.Context()))

	pkg, rows := readArticles(t, run.packageDir(t))
	assert.Equal(t, "jsonl", pkg.Resources[0].Format)
	assert.Equal(t, "articles.jsonl", pkg.Resources[0].Path)
	assert.Equal(t, []string{"A", "B"}, rows.byURL["https://example.com/1"].Metadata.Authors)
}

func TestPreprocess_RefusesAMistypedIdStrategyKey(t *testing.T) {
	run := newPreprocessRun(t, mappingWith(t, "idStrategy:", "idStrategey:"),
		csvFile("part.csv", "title,text,url\nOne,Body,https://example.com/1\n"),
	)

	err := run.execute(t.Context())

	require.ErrorContains(t, err, "idStrategey")
	assert.NoDirExists(t, run.outputRoot)
}
