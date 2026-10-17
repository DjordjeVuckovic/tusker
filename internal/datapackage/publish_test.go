package datapackage_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/datapackage"
)

func stagedArticles(t *testing.T, root, content, commit string) (staging string, pkg datapackage.Package) {
	t.Helper()
	staging, err := os.MkdirTemp(root, "staging-")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, staging, "articles.jsonl", content)
	return staging, datapackage.Package{
		Name:       "cc-news-canonical",
		ProducedBy: &datapackage.ProducedBy{Tool: "datapipe preprocess", Commit: commit},
		Resources:  []datapackage.Resource{{Name: datapackage.ArticlesResource, Path: "articles.jsonl", Format: "jsonl"}},
	}
}

func absent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s still exists", path)
	}
}

func TestPublishMovesTheStagedPackageToTheVersionOfItsBytes(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "canonical")
	staging, pkg := stagedArticles(t, root, "hello\n", "c4ecbe3")

	published, err := datapackage.Publish(context.Background(), pkg, datapackage.PublishOptions{StagingDir: staging, ParentDir: parent})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if want := filepath.Join(parent, "5891b5b5"); published.Dir != want || published.Package.Version != "5891b5b5" || published.Existing {
		t.Errorf("published = %s version %q existing %v, want %s version 5891b5b5, new", published.Dir, published.Package.Version, published.Existing, want)
	}
	absent(t, staging)
	opened, err := datapackage.Open(context.Background(), published.Dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got, err := readResource(context.Background(), opened, datapackage.ArticlesResource); err != nil || string(got) != "hello\n" {
		t.Errorf("articles = %q, %v", got, err)
	}
}

func TestPublishGivesDifferentBytesTheirOwnFolder(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "canonical")
	for content, version := range map[string]string{"hello\n": "5891b5b5", "hello, again\n": "aeac3c79"} {
		staging, pkg := stagedArticles(t, root, content, "c4ecbe3")
		published, err := datapackage.Publish(context.Background(), pkg, datapackage.PublishOptions{StagingDir: staging, ParentDir: parent})
		if err != nil {
			t.Fatalf("Publish(%q): %v", content, err)
		}
		if published.Dir != filepath.Join(parent, version) {
			t.Errorf("Publish(%q) landed in %s, want version %s", content, published.Dir, version)
		}
	}
}

func TestPublishKeepsAPackageAlreadyPublished(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "canonical")
	ctx := context.Background()
	first, pkg := stagedArticles(t, root, "hello\n", "c4ecbe3")
	published, err := datapackage.Publish(ctx, pkg, datapackage.PublishOptions{StagingDir: first, ParentDir: parent})
	if err != nil {
		t.Fatalf("first Publish: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(published.Dir, datapackage.DescriptorName))
	if err != nil {
		t.Fatal(err)
	}

	second, rerun := stagedArticles(t, root, "hello\n", "4be80e0")
	again, err := datapackage.Publish(ctx, rerun, datapackage.PublishOptions{StagingDir: second, ParentDir: parent})
	if err != nil {
		t.Fatalf("second Publish: %v", err)
	}

	if !again.Existing || again.Dir != published.Dir || again.Package.ProducedBy.Commit != "c4ecbe3" {
		t.Errorf("rerun = %s existing %v commit %q, want the existing %s from c4ecbe3", again.Dir, again.Existing, again.Package.ProducedBy.Commit, published.Dir)
	}
	absent(t, second)
	after, err := os.ReadFile(filepath.Join(published.Dir, datapackage.DescriptorName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("the published descriptor changed:\n%s", after)
	}
}

func TestPublishRefusesWhatItCannotPublish(t *testing.T) {
	tests := map[string]func(staging string, pkg *datapackage.Package) datapackage.PublishOptions{
		"two resources": func(staging string, pkg *datapackage.Package) datapackage.PublishOptions {
			writeFile(t, staging, "more.jsonl", "more\n")
			pkg.Resources = append(pkg.Resources, datapackage.Resource{Name: "more", Path: "more.jsonl", Format: "jsonl"})
			return datapackage.PublishOptions{StagingDir: staging, ParentDir: filepath.Join(filepath.Dir(staging), "canonical")}
		},
		"a version already set": func(staging string, pkg *datapackage.Package) datapackage.PublishOptions {
			pkg.Version = "a1b2c3d4"
			return datapackage.PublishOptions{StagingDir: staging, ParentDir: filepath.Join(filepath.Dir(staging), "canonical")}
		},
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			staging, pkg := stagedArticles(t, t.TempDir(), "hello\n", "c4ecbe3")
			opts := setup(staging, &pkg)

			if _, err := datapackage.Publish(context.Background(), pkg, opts); err == nil {
				t.Fatal("Publish succeeded")
			}
			if _, err := os.Stat(filepath.Join(staging, "articles.jsonl")); err != nil {
				t.Errorf("the staged resource is gone: %v", err)
			}
			absent(t, filepath.Join(opts.ParentDir, "5891b5b5"))
		})
	}
}

func TestPublishStopsOnCancelWithoutPublishing(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "canonical")
	staging, pkg := stagedArticles(t, root, "hello\n", "c4ecbe3")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := datapackage.Publish(ctx, pkg, datapackage.PublishOptions{StagingDir: staging, ParentDir: parent})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	absent(t, filepath.Join(parent, "5891b5b5"))
}

// Discarding a staging folder that contains the parent would delete what is
// already published there.
func TestPublishRefusesAParentInsideTheStagingFolder(t *testing.T) {
	ctx := context.Background()
	staging, pkg := stagedArticles(t, t.TempDir(), "hello\n", "c4ecbe3")
	parent := filepath.Join(staging, "canonical")
	earlier, earlierPkg := stagedArticles(t, t.TempDir(), "hello\n", "c4ecbe3")
	published, err := datapackage.Publish(ctx, earlierPkg, datapackage.PublishOptions{StagingDir: earlier, ParentDir: parent})
	if err != nil {
		t.Fatalf("earlier Publish: %v", err)
	}

	if _, err := datapackage.Publish(ctx, pkg, datapackage.PublishOptions{StagingDir: staging, ParentDir: parent}); err == nil {
		t.Fatal("Publish succeeded with the parent inside the staging folder")
	}
	if _, err := os.Stat(filepath.Join(published.Dir, datapackage.DescriptorName)); err != nil {
		t.Errorf("the package published earlier is gone: %v", err)
	}
}
