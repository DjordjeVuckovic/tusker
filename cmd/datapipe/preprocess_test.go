package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/datapackage"
	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/DjordjeVuckovic/tusker/internal/types/language"
	"github.com/google/uuid"
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

// rawVersion is the top-level version the fetch scripts record: the Hugging
// Face commit sha, or the Kaggle dataset version.
const rawVersion = "0123abcd"

// writeRawPackage lays out a raw package the way the fetch scripts do. An
// empty version leaves the descriptor without one, as older fetches did.
func writeRawPackage(t *testing.T, dir, version string, files ...rawFile) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	pkg := datapackage.Package{
		Name:    "test-news-raw",
		Version: version,
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
	writeRawPackage(t, run.rawDir, rawVersion, files...)
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
		Version:   rawVersion,
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

func TestPreprocess_DerivesEachIDFromTheNormalisedURL(t *testing.T) {
	run := newPreprocessRun(t, ccNewsLikeMapping,
		csvFile("part.csv", "title,text,url\nSpaced,Body,  https://example.com/a b \n"),
	)

	require.NoError(t, run.execute(t.Context()))

	pkg, rows := readArticles(t, run.packageDir(t))
	require.Equal(t, []string{"https://example.com/a%20b"}, rows.urls)
	require.NotNil(t, pkg.IDStrategy)
	assert.Equal(t, "uuidv5", string(pkg.IDStrategy.Kind))
	assert.Equal(t, "url", pkg.IDStrategy.Source)
	namespace, err := uuid.Parse(pkg.IDStrategy.Namespace)
	require.NoError(t, err)
	assert.Equal(t, uuid.NewSHA1(namespace, []byte("https://example.com/a%20b")), rows.byURL["https://example.com/a%20b"].ID)
}

func TestPreprocess_KeepsTheFirstOfRecordsSharingAURL(t *testing.T) {
	run := newPreprocessRun(t, ccNewsLikeMapping,
		csvFile("a.csv", "title,text,url\nFirst,Body,https://example.com/story\nOther,Body,https://example.com/other\n"),
		csvFile("b.csv", "title,text,url\nRepeat,Body, https://example.com/story\nAgain,Body,https://example.com/other\n"),
	)

	require.NoError(t, run.execute(t.Context()))

	pkg, rows := readArticles(t, run.packageDir(t))
	assert.Equal(t, []string{"https://example.com/story", "https://example.com/other"}, rows.urls)
	assert.Equal(t, "First", rows.byURL["https://example.com/story"].Title)
	assert.Equal(t, "Other", rows.byURL["https://example.com/other"].Title)
	assert.Equal(t, &datapackage.Report{Read: 4, Written: 2, Deduped: 2}, pkg.Report)
}

func TestPreprocess_DerivesThePublisherDomainFromTheURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{url: "https://uk.reuters.com/world/storm", want: "reuters.com"},
		{url: "https://WWW.Reuters.com/markets", want: "reuters.com"},
		{url: "https://news.bbc.co.uk/1", want: "bbc.co.uk"},
		{url: "https://x.blogspot.com/post", want: "x.blogspot.com"},
		{url: "http://192.168.0.7/feed", want: "192.168.0.7"},
		{url: "http://localhost:8080/page", want: "localhost"},
	}
	var csv strings.Builder
	csv.WriteString("title,text,url\n")
	for _, tt := range tests {
		csv.WriteString("Title,Body," + tt.url + "\n")
	}
	run := newPreprocessRun(t, ccNewsLikeMapping, csvFile("part.csv", csv.String()))

	require.NoError(t, run.execute(t.Context()))

	_, rows := readArticles(t, run.packageDir(t))
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.want, rows.byURL[tt.url].PublisherDomain)
		})
	}
}

// freeze makes a package folder and its files read-only, so any later write
// into it fails instead of passing unnoticed.
func freeze(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		require.NoError(t, os.Chmod(filepath.Join(dir, e.Name()), 0o444))
	}
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o755)
		for _, e := range entries {
			_ = os.Chmod(filepath.Join(dir, e.Name()), 0o644)
		}
	})
}

func TestPreprocess_RerunOverTheSameInputsKeepsTheExistingPackage(t *testing.T) {
	run := newPreprocessRun(t, ccNewsLikeMapping,
		csvFile("part.csv", "title,text,url\nOne,Body,https://example.com/1\nTwo,Body,https://example.com/2\n"),
	)
	require.NoError(t, run.execute(t.Context()))
	dir := run.packageDir(t)
	resourcePath := filepath.Join(dir, "articles.parquet")
	firstHash := sha256Of(t, resourcePath)
	before, err := os.Stat(resourcePath)
	require.NoError(t, err)
	freeze(t, dir)

	require.NoError(t, run.execute(t.Context()))

	assert.Equal(t, dir, run.packageDir(t))
	after, err := os.Stat(resourcePath)
	require.NoError(t, err)
	assert.True(t, os.SameFile(before, after), "the resource file was replaced")
	assert.Equal(t, firstHash, sha256Of(t, resourcePath))
}

func TestPreprocess_TwoRunsOverOneRawPackageWriteIdenticalBytes(t *testing.T) {
	run := newPreprocessRun(t, ccNewsLikeMapping,
		parquetFile(t, "train-0.parquet",
			rawShardRow{Title: "One", Text: "Body", URL: "https://example.com/1", Date: "2019-03-04 05:06:07", Authors: "A; B", Words: "3"},
			rawShardRow{Title: "Two", Text: "Body", URL: "https://example.com/2"},
			rawShardRow{Title: "Three", Text: "Body", URL: "https://example.com/3"},
		),
	)
	second := run
	second.outputRoot = filepath.Join(t.TempDir(), "canonical")

	require.NoError(t, run.execute(t.Context()))
	require.NoError(t, second.execute(t.Context()))

	first, other := run.packageDir(t), second.packageDir(t)
	assert.Equal(t, filepath.Base(first), filepath.Base(other))
	assert.Equal(t, sha256Of(t, filepath.Join(first, "articles.parquet")), sha256Of(t, filepath.Join(other, "articles.parquet")))
}

func TestPreprocess_RefusesAVersionThatDescribesOtherContent(t *testing.T) {
	run := newPreprocessRun(t, ccNewsLikeMapping,
		csvFile("part.csv", "title,text,url\nOne,Body,https://example.com/1\n"),
	)
	require.NoError(t, run.execute(t.Context()))
	dir := run.packageDir(t)
	descriptorPath := filepath.Join(dir, datapackage.DescriptorName)
	descriptor, err := os.ReadFile(descriptorPath)
	require.NoError(t, err)
	actualHash := sha256Of(t, filepath.Join(dir, "articles.parquet"))
	tampered := strings.Replace(string(descriptor), actualHash, "sha256:"+strings.Repeat("0", 64), 1)
	require.NotEqual(t, string(descriptor), tampered)
	require.NoError(t, os.WriteFile(descriptorPath, []byte(tampered), 0o644))

	err = run.execute(t.Context())

	var conflict *datapackage.DescriptorConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, dir, run.packageDir(t), "a refused run leaves nothing behind")
	kept, err := os.ReadFile(descriptorPath)
	require.NoError(t, err)
	assert.Equal(t, tampered, string(kept))
	assert.Equal(t, actualHash, sha256Of(t, filepath.Join(dir, "articles.parquet")))
}

func TestPreprocess_CancelledRunLeavesNoPackage(t *testing.T) {
	rawDir := filepath.Join(t.TempDir(), "raw")
	writeRawPackage(t, rawDir, rawVersion,
		csvFile("a.csv", "title,text,url\nOne,Body,https://example.com/1\n"),
		csvFile("b.csv", "title,text,url\nTwo,Body,https://example.com/2\n"),
	)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	files := http.FileServer(http.Dir(rawDir))
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/b.csv") {
			cancel()
		}
		files.ServeHTTP(w, r)
	}))
	defer srv.Close()
	mappingPath := filepath.Join(t.TempDir(), "mapping.yaml")
	require.NoError(t, os.WriteFile(mappingPath, []byte(ccNewsLikeMapping), 0o644))
	outputRoot := filepath.Join(t.TempDir(), "canonical")

	err := runPreprocess(ctx, io.Discard, preprocessConfig{
		InputPath:   srv.URL + "/datapackage.json",
		OutputPath:  outputRoot,
		MappingPath: mappingPath,
		Packages:    datapackage.Options{HTTPClient: srv.Client(), TempDir: t.TempDir()},
	})

	require.ErrorIs(t, err, context.Canceled)
	entries, err := os.ReadDir(outputRoot)
	if !errors.Is(err, fs.ErrNotExist) {
		require.NoError(t, err)
	}
	assert.Empty(t, entries, "no package and no temporary folder remain")
}

func TestPreprocess_KeepsTheIDsADatasetCarries(t *testing.T) {
	mapping := strings.Replace(
		mappingWith(t, "idStrategy:\n  kind: uuidv5\n  source: url\n", ""),
		"fieldMappings:\n", "fieldMappings:\n  - {source: uid, sourceType: uuid, target: ID, required: true}\n", 1)
	run := newPreprocessRun(t, mapping, csvFile("part.csv", "uid,title,text,url\n"+
		"3f2b8c1e-0d4a-4e7b-9c55-1a2b3c4d5e6f,One,Body,https://example.com/1\n"+
		"9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d,Two,Body,https://example.com/2\n"))

	require.NoError(t, run.execute(t.Context()))

	pkg, rows := readArticles(t, run.packageDir(t))
	assert.Nil(t, pkg.IDStrategy)
	assert.Equal(t, uuid.MustParse("3f2b8c1e-0d4a-4e7b-9c55-1a2b3c4d5e6f"), rows.byURL["https://example.com/1"].ID)
	assert.Equal(t, uuid.MustParse("9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d"), rows.byURL["https://example.com/2"].ID)
}

func TestPreprocess_DerivesIDsFromTheURLWhenTheMappingNamesNoStrategy(t *testing.T) {
	run := newPreprocessRun(t, mappingWith(t, "idStrategy:\n  kind: uuidv5\n  source: url\n", ""),
		csvFile("part.csv", "title,text,url\nOne,Body,https://example.com/1\n"),
	)

	require.NoError(t, run.execute(t.Context()))

	pkg, rows := readArticles(t, run.packageDir(t))
	require.NotNil(t, pkg.IDStrategy)
	assert.Equal(t, "uuidv5", string(pkg.IDStrategy.Kind))
	assert.Equal(t, "url", pkg.IDStrategy.Source)
	namespace, err := uuid.Parse(pkg.IDStrategy.Namespace)
	require.NoError(t, err)
	assert.Equal(t, uuid.NewSHA1(namespace, []byte("https://example.com/1")), rows.byURL["https://example.com/1"].ID)
}

func TestPreprocess_OmitsTheRawVersionWhenTheRawPackageHasNone(t *testing.T) {
	run := newPreprocessRun(t, ccNewsLikeMapping, csvFile("part.csv", "title,text,url\nOne,Body,https://example.com/1\n"))
	require.NoError(t, os.RemoveAll(run.rawDir))
	writeRawPackage(t, run.rawDir, "", csvFile("part.csv", "title,text,url\nOne,Body,https://example.com/1\n"))

	require.NoError(t, run.execute(t.Context()))

	descriptor, err := os.ReadFile(filepath.Join(run.packageDir(t), datapackage.DescriptorName))
	require.NoError(t, err)
	var doc struct {
		Raw map[string]json.RawMessage `json:"raw"`
	}
	require.NoError(t, json.Unmarshal(descriptor, &doc))
	assert.Contains(t, doc.Raw, "name")
	assert.NotContains(t, doc.Raw, "version")
}
