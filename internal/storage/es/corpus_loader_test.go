package es

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/embedding"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/DjordjeVuckovic/tusker/internal/types/language"
	pkgtesting "github.com/DjordjeVuckovic/tusker/pkg/testing"
)

const loadTestModelTag = "qwen3-embedding:0.6b@1024"

func TestCorpusLoader_LoadCorpus(t *testing.T) {
	ctx := context.Background()
	container := pkgtesting.NewESContainer(ctx, t)

	t.Run("declares the contract's index settings and mapping", func(t *testing.T) {
		index := "articles_settings_test"
		articles := testArticles(3)
		if _, err := loadCorpus(t, container.Address, index, corpusOf(articles, nil), 100); err != nil {
			t.Fatalf("LoadCorpus: %v", err)
		}

		var settings map[string]struct {
			Settings map[string]any `json:"settings"`
		}
		getJSON(t, container.Address+"/"+index+"/_settings?flat_settings=true", &settings)
		got := settings[index].Settings
		for key, want := range map[string]string{
			"index.number_of_shards":   "1",
			"index.number_of_replicas": "0",
			"index.max_result_window":  "10001",
			"index.refresh_interval":   "1s",
		} {
			if got[key] != any(want) {
				t.Errorf("%s = %v, want %q", key, got[key], want)
			}
		}

		var mappings map[string]struct {
			Mappings struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"mappings"`
		}
		getJSON(t, container.Address+"/"+index+"/_mapping", &mappings)
		properties := mappings[index].Mappings.Properties
		want := map[string]string{
			"title":            `{"type":"text","analyzer":"english_analyzer","fields":{"keyword":{"type":"keyword"}}}`,
			"description":      `{"type":"text","analyzer":"english_analyzer"}`,
			"content":          `{"type":"text","analyzer":"english_analyzer"}`,
			"id":               `{"type":"keyword"}`,
			"url":              `{"type":"keyword"}`,
			"language":         `{"type":"keyword"}`,
			"publisher_domain": `{"type":"keyword"}`,
			"embedding_model":  `{"type":"keyword"}`,
			"published_at":     `{"type":"date"}`,
			"metadata":         `{"type":"object","enabled":false}`,
			"embedding":        `{"type":"dense_vector","dims":1024,"index":true,"similarity":"cosine","index_options":{"type":"hnsw","m":16,"ef_construction":64}}`,
		}
		for field, mapping := range want {
			assertJSONEqual(t, field, properties[field], mapping)
		}
		for field := range properties {
			if _, ok := want[field]; !ok {
				t.Errorf("mapping has undeclared field %q", field)
			}
		}

		var analyzed struct {
			Tokens []struct {
				Token string `json:"token"`
			} `json:"tokens"`
		}
		postJSON(t, container.Address+"/"+index+"/_analyze", `{"field":"content","text":"The news skies"}`, &analyzed)
		var tokens []string
		for _, token := range analyzed.Tokens {
			tokens = append(tokens, token.Token)
		}
		if want := []string{"news", "sky"}; !slices.Equal(tokens, want) {
			t.Errorf("content analyzes %q to %q, want %q", "The news skies", tokens, want)
		}
	})

	t.Run("stores every article with its own vector and model tag", func(t *testing.T) {
		index := "articles_loaded_test"
		articles := testArticles(2000)
		if _, err := loadCorpus(t, container.Address, index, corpusOf(articles, testVectors(articles)), 500); err != nil {
			t.Fatalf("LoadCorpus: %v", err)
		}

		stored := fetchByID(t, container.Address, index, articles)
		for i, a := range articles {
			doc, ok := stored[a.ID.String()]
			if !ok {
				t.Errorf("article %s not stored", a.ID)
				continue
			}
			want := storedDocument{
				ID:              a.ID.String(),
				URL:             a.URL,
				Title:           a.Title,
				Description:     a.Description,
				Content:         a.Content,
				PublishedAt:     a.PublishedAt.Format(time.RFC3339),
				Language:        "eng",
				PublisherDomain: a.PublisherDomain,
				Metadata:        map[string]any{"publisherName": "Example News", "authors": []any{"A. Writer"}},
				Embedding:       vectorFor(i),
				EmbeddingModel:  loadTestModelTag,
			}
			if diff := cmpDocuments(doc, want); diff != "" {
				t.Errorf("article %d (%s): %s", i, a.ID, diff)
			}
		}
	})

	for _, tc := range []struct {
		name    string
		index   string
		corrupt func(articles []document.Article, vecs []embedding.Vec)
	}{
		{
			name:    "a NaN vector",
			index:   "articles_nan_vector_test",
			corrupt: func(_ []document.Article, vecs []embedding.Vec) { vecs[3].Embedding[0] = float32(math.NaN()) },
		},
		{
			name:  "a publication date past year 9999",
			index: "articles_year_10000_test",
			corrupt: func(articles []document.Article, _ []embedding.Vec) {
				articles[3].PublishedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
			},
		},
		{
			name:    "a vector elasticsearch rejects for its width",
			index:   "articles_short_vector_test",
			corrupt: func(_ []document.Article, vecs []embedding.Vec) { vecs[3].Embedding = vecs[3].Embedding[:3] },
		},
	} {
		t.Run(tc.name+" fails the load, names the article and leaves no index", func(t *testing.T) {
			index := tc.index
			articles := testArticles(5)
			vecs := testVectors(articles)
			tc.corrupt(articles, vecs)

			_, err := loadCorpus(t, container.Address, index, corpusOf(articles, vecs), 2)
			var failed *DocumentsFailedError
			if !errors.As(err, &failed) {
				t.Fatalf("LoadCorpus error = %v, want *DocumentsFailedError", err)
			}
			if failed.Failed != 1 || len(failed.Failures) != 1 || failed.Failures[0].ID != articles[3].ID {
				t.Errorf("failures = %d %+v, want only article %s", failed.Failed, failed.Failures, articles[3].ID)
			}
			if !strings.Contains(err.Error(), articles[3].ID.String()) {
				t.Errorf("error %q does not name article %s", err, articles[3].ID)
			}
			assertNoIndex(t, container.Address, index)
		})
	}

	for _, tc := range []struct {
		name    string
		index   string
		pairing func(vecs []embedding.Vec) []embedding.Vec
	}{
		{
			name:    "vectors out of article order",
			index:   "articles_swapped_vectors_test",
			pairing: func(vecs []embedding.Vec) []embedding.Vec { vecs[1], vecs[2] = vecs[2], vecs[1]; return vecs },
		},
		{
			name:    "fewer vectors than articles",
			index:   "articles_missing_vector_test",
			pairing: func(vecs []embedding.Vec) []embedding.Vec { return vecs[:len(vecs)-1] },
		},
		{
			name:  "more vectors than articles",
			index: "articles_extra_vector_test",
			pairing: func(vecs []embedding.Vec) []embedding.Vec {
				return append(vecs, embedding.Vec{ID: document.DeriveArticleID("https://example.com/extra"), Embedding: vectorFor(9)})
			},
		},
	} {
		t.Run(tc.name+" fail the load and leave no index", func(t *testing.T) {
			articles := testArticles(4)
			if _, err := loadCorpus(t, container.Address, tc.index, corpusOf(articles, tc.pairing(testVectors(articles))), 2); err == nil {
				t.Fatal("LoadCorpus paired articles with vectors that are not theirs")
			}
			assertNoIndex(t, container.Address, tc.index)
		})
	}

	t.Run("refuses a corpus language with no declared analyzer and creates no index", func(t *testing.T) {
		index := "articles_language_test"
		c := corpusOf(testArticles(1), nil)
		c.Language = "srp"
		if _, err := loadCorpus(t, container.Address, index, c, 100); err == nil {
			t.Fatal("LoadCorpus accepted a Serbian corpus into an English-only index")
		}
		assertNoIndex(t, container.Address, index)
	})

	t.Run("refuses an existing index and leaves its documents alone", func(t *testing.T) {
		index := "articles_existing_test"
		var created map[string]any
		sendJSON(t, http.MethodPut, container.Address+"/"+index+"/_doc/kept?refresh=true", `{"title":"kept"}`, &created)

		_, err := loadCorpus(t, container.Address, index, corpusOf(testArticles(2), nil), 100)
		var exists *IndexExistsError
		if !errors.As(err, &exists) {
			t.Fatalf("LoadCorpus error = %v, want *IndexExistsError", err)
		}
		var kept struct {
			Found bool `json:"found"`
		}
		getJSON(t, container.Address+"/"+index+"/_doc/kept", &kept)
		if !kept.Found {
			t.Error("the existing document is gone after a refused load")
		}
	})
}

type storedDocument struct {
	ID              string         `json:"id"`
	URL             string         `json:"url"`
	Title           string         `json:"title"`
	Description     string         `json:"description"`
	Content         string         `json:"content"`
	PublishedAt     string         `json:"published_at"`
	Language        string         `json:"language"`
	PublisherDomain string         `json:"publisher_domain"`
	Metadata        map[string]any `json:"metadata"`
	Embedding       []float32      `json:"embedding"`
	EmbeddingModel  string         `json:"embedding_model"`
}

func cmpDocuments(got, want storedDocument) string {
	if !slices.Equal(got.Embedding, want.Embedding) {
		return "carries another vector than its own"
	}
	got.Embedding, want.Embedding = nil, nil
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		return fmt.Sprintf("stored %s, want %s", gotJSON, wantJSON)
	}
	return ""
}

// fetchByID reads the articles' documents back with _mget, keyed by _id.
func fetchByID(t *testing.T, address, index string, articles []document.Article) map[string]storedDocument {
	t.Helper()
	ids := make([]string, len(articles))
	for i, a := range articles {
		ids[i] = a.ID.String()
	}
	body, err := json.Marshal(map[string]any{"ids": ids})
	if err != nil {
		t.Fatalf("marshal ids: %v", err)
	}
	var res struct {
		Docs []struct {
			ID     string         `json:"_id"`
			Found  bool           `json:"found"`
			Source storedDocument `json:"_source"`
		} `json:"docs"`
	}
	postJSON(t, address+"/"+index+"/_mget", string(body), &res)
	stored := make(map[string]storedDocument, len(res.Docs))
	for _, doc := range res.Docs {
		if doc.Found {
			stored[doc.ID] = doc.Source
		}
	}
	return stored
}

func assertNoIndex(t *testing.T, address, index string) {
	t.Helper()
	res, err := http.Head(address + "/" + index)
	if err != nil {
		t.Fatalf("HEAD %s: %v", index, err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("index %s exists (status %d), want none left behind", index, res.StatusCode)
	}
}

func loadCorpus(t *testing.T, address, index string, c storage.Corpus, batchSize int) (storage.LoadRecord, error) {
	t.Helper()
	client := newTestClient(t, ClientConfig{Addresses: []string{address}, IndexName: index})
	return NewCorpusLoader(client).LoadCorpus(context.Background(), c, storage.LoadOptions{BatchSize: batchSize})
}

func testArticles(n int) []document.Article {
	articles := make([]document.Article, n)
	for i := range articles {
		url := fmt.Sprintf("https://example.com/news/%d", i)
		articles[i] = document.Article{
			ID:              document.DeriveArticleID(url),
			URL:             url,
			Title:           fmt.Sprintf("Headline number %d", i),
			Description:     fmt.Sprintf("Lead paragraph %d", i),
			Content:         fmt.Sprintf("Body text of article %d", i),
			PublishedAt:     time.Date(2024, 1, 1+i%28, 12, 0, 0, 0, time.UTC),
			Language:        language.English,
			PublisherDomain: "example.com",
			Metadata:        document.Metadata{PublisherName: "Example News", Authors: []string{"A. Writer"}},
		}
	}
	return articles
}

// vectorFor encodes the article's position in every component, so a vector
// stored on the wrong document is told apart from its own.
func vectorFor(i int) []float32 {
	v := make([]float32, EmbeddingDims)
	for j := range v {
		v[j] = float32(i+1) / float32(j+1)
	}
	return v
}

func testVectors(articles []document.Article) []embedding.Vec {
	vecs := make([]embedding.Vec, len(articles))
	for i, a := range articles {
		vecs[i] = embedding.Vec{ID: a.ID, Model: loadTestModelTag, Embedding: vectorFor(i)}
	}
	return vecs
}

func corpusOf(articles []document.Article, vecs []embedding.Vec) storage.Corpus {
	c := storage.Corpus{
		Articles:  seqOf(articles),
		Language:  language.English,
		Canonical: storage.PackageRef{Name: "cc-news", Version: "article-v2-0000test", ResourceHash: "sha256:" + fmt.Sprintf("%064d", 0)},
	}
	if vecs != nil {
		c.Vectors = seqOf(vecs)
		c.ModelTag = loadTestModelTag
		c.Embeddings = &storage.PackageRef{Name: "cc-news-embeddings", Version: "lead-v1-1024-0000test", ResourceHash: "sha256:" + fmt.Sprintf("%064d", 1)}
	}
	return c
}

func seqOf[T any](items []T) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for _, item := range items {
			if !yield(item, nil) {
				return
			}
		}
	}
}

func getJSON(t *testing.T, url string, out any) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	decodeResponse(t, url, res, out)
}

func postJSON(t *testing.T, url, body string, out any) {
	t.Helper()
	sendJSON(t, http.MethodPost, url, body, out)
}

func sendJSON(t *testing.T, method, url, body string, out any) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	decodeResponse(t, url, res, out)
}

func decodeResponse(t *testing.T, url string, res *http.Response, out any) {
	t.Helper()
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	if res.StatusCode >= http.StatusMultipleChoices {
		t.Fatalf("%s: status %d: %s", url, res.StatusCode, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("decode %s: %v: %s", url, err, body)
	}
}

func assertJSONEqual(t *testing.T, name string, got json.RawMessage, want string) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Errorf("%s: mapping %s is not JSON: %v", name, got, err)
		return
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("%s: want %s is not JSON: %v", name, want, err)
	}
	gotBytes, _ := json.Marshal(gotValue)
	wantBytes, _ := json.Marshal(wantValue)
	if string(gotBytes) != string(wantBytes) {
		t.Errorf("%s = %s, want %s", name, gotBytes, wantBytes)
	}
}
