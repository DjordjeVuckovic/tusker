package es

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
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

	t.Run("refuses a corpus language with no declared analyzer and creates no index", func(t *testing.T) {
		index := "articles_language_test"
		c := corpusOf(testArticles(1), nil)
		c.Language = "srp"
		if _, err := loadCorpus(t, container.Address, index, c, 100); err == nil {
			t.Fatal("LoadCorpus accepted a Serbian corpus into an English-only index")
		}
		assertNoIndex(t, container.Address, index)
	})
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
	res, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
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
	if res.StatusCode != http.StatusOK {
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
