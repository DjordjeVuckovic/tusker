package datapackage_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/datapackage"
)

func queryVectorsPackage() datapackage.Package {
	return datapackage.Package{
		Name:    "cc-news-fts-query-vectors",
		Version: "query-instruct-v1-1024-938b2254",
		Embedding: &datapackage.Embedding{
			Model: "qwen3-embedding:0.6b", HuggingFaceModel: "Qwen/Qwen3-Embedding-0.6B", Revision: "abc123",
			Tag: "qwen3-embedding:0.6b@1024", Dim: 1024, Recipe: "query-instruct-v1", Pooling: "last_token",
			Normalized: true, MaxTokens: 512,
		},
		Suite: &datapackage.Suite{ID: "cc-news-fts", Hash: "sha256:" + zeros64},
		Resources: []datapackage.Resource{
			{Name: datapackage.QueryVectorsResource, Path: "query-vectors.parquet", Format: "parquet"},
		},
	}
}

func TestQueryVectorsSuiteRoundTripsThroughWriteAndOpen(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "query-vectors.parquet", "not really vectors")
	written, err := datapackage.Write(context.Background(), dir, queryVectorsPackage())
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	doc := readDescriptorJSON(t, dir)
	if lookup(doc, "suite/id") != "cc-news-fts" || lookup(doc, "suite/hash") != "sha256:"+zeros64 {
		t.Errorf("suite = %v, want {id: cc-news-fts, hash: sha256:<zeros>}", doc["suite"])
	}
	opened, err := datapackage.Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !reflect.DeepEqual(opened.Package, written) {
		t.Errorf("descriptor differs:\n got %#v\nwant %#v", opened.Package, written)
	}
	if opened.Package.Suite == nil || *opened.Package.Suite != (datapackage.Suite{ID: "cc-news-fts", Hash: "sha256:" + zeros64}) {
		t.Errorf("opened suite = %+v", opened.Package.Suite)
	}
}

func TestSuiteIsRefusedOutsideAQueryVectorsPackage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "articles.parquet", "not really parquet")
	writeFile(t, dir, "embeddings.parquet", "not really vectors")
	pkg := fullPackage()
	pkg.Suite = &datapackage.Suite{ID: "cc-news-fts", Hash: "sha256:" + zeros64}

	if _, err := datapackage.Write(context.Background(), dir, pkg); err == nil {
		t.Error("Write accepted a suite on a package without a query-vectors resource")
	}
}
