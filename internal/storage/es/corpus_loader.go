package es

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"strings"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/embedding"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/DjordjeVuckovic/tusker/internal/types/language"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/google/uuid"
)

const (
	// refreshIntervalAfterLoad is declared rather than reset to null, so the
	// interval a search sees is the one this build states, not a vendor default.
	refreshIntervalAfterLoad = "1s"
	cleanupTimeout           = 30 * time.Second
	maxFailuresReported      = 10
)

// CorpusLoader builds the article index from scratch in a single bulk pass, each
// article written together with its vector.
type CorpusLoader struct {
	client *Client
}

func NewCorpusLoader(client *Client) *CorpusLoader {
	return &CorpusLoader{client: client}
}

// IndexExistsError refuses a load into an index that already exists, since its
// analyzer and mapping may predate this build. Delete the index to reload it.
type IndexExistsError struct {
	Index string
}

func (e *IndexExistsError) Error() string {
	return fmt.Sprintf("index %s already exists: delete it to reload the corpus", e.Index)
}

// DocumentFailure is one article Elasticsearch did not store, or that could not
// be encoded for it.
type DocumentFailure struct {
	ID     uuid.UUID
	Reason string
}

// DocumentsFailedError fails a load in which any article was not stored.
// Failures holds the first few; Failed counts them all.
type DocumentsFailedError struct {
	Index    string
	Failed   int
	Failures []DocumentFailure
}

func (e *DocumentsFailedError) Error() string {
	examples := make([]string, len(e.Failures))
	for i, f := range e.Failures {
		examples[i] = fmt.Sprintf("%s: %s", f.ID, f.Reason)
	}
	return fmt.Sprintf("load into %s: %d articles not stored, first %d: %s",
		e.Index, e.Failed, len(e.Failures), strings.Join(examples, "; "))
}

func (e *DocumentsFailedError) add(id uuid.UUID, reason string) {
	e.Failed++
	if len(e.Failures) < maxFailuresReported {
		e.Failures = append(e.Failures, DocumentFailure{ID: id, Reason: reason})
	}
}

// LoadCorpus creates the index and fails if it exists, then bulk-writes every
// article with its vector. Any failure deletes the index it created, so a
// failed load leaves nothing behind.
func (l *CorpusLoader) LoadCorpus(ctx context.Context, c storage.Corpus, opts storage.LoadOptions) (record storage.LoadRecord, err error) {
	index := l.client.indexName
	if index == "" {
		return storage.LoadRecord{}, errors.New("load corpus: no index name configured")
	}
	if err := checkCorpus(c, opts); err != nil {
		return storage.LoadRecord{}, fmt.Errorf("load corpus into %s: %w", index, err)
	}

	if err := l.createIndex(ctx, c.Language); err != nil {
		return storage.LoadRecord{}, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, l.deleteIndex(ctx))
		}
	}()

	written, err := l.writeDocuments(ctx, c, opts.BatchSize)
	if err != nil {
		return storage.LoadRecord{}, err
	}
	if err := l.restoreRefresh(ctx); err != nil {
		return storage.LoadRecord{}, err
	}
	return storage.LoadRecord{Articles: written.articles, Vectors: written.vectors}, nil
}

func checkCorpus(c storage.Corpus, opts storage.LoadOptions) error {
	if c.Language != language.English {
		return fmt.Errorf("the index analyzes %q only, the corpus is %q", language.English, c.Language)
	}
	if opts.BatchSize <= 0 {
		return fmt.Errorf("batch size %d: want a positive number of articles per bulk request", opts.BatchSize)
	}
	if opts.SkipIndex {
		return errors.New("elasticsearch builds its search structures as it writes, so it cannot skip indexing")
	}
	if (c.Vectors == nil) != (c.Embeddings == nil) {
		return errors.New("vectors and their embeddings package must be given together")
	}
	if c.Vectors != nil && c.ModelTag == "" {
		return errors.New("vectors given without the model tag that labels them")
	}
	return nil
}

func (l *CorpusLoader) createIndex(ctx context.Context, lang language.Code) error {
	index := l.client.indexName
	builder := NewIndexBuilder()
	settings := builder.buildSettings()
	settings.RefreshInterval = "-1"
	mappings := builder.buildMapping(lang.Analysis().ElasticsearchAnalyzer)
	if _, err := l.client.typed.Indices.Create(index).Settings(&settings).Mappings(&mappings).Do(ctx); err != nil {
		var esErr *types.ElasticsearchError
		if errors.As(err, &esErr) && esErr.ErrorCause.Type == "resource_already_exists_exception" {
			return &IndexExistsError{Index: index}
		}
		return fmt.Errorf("create index %s: %w", index, err)
	}
	return nil
}

// deleteIndex runs even when ctx is cancelled, since it cleans up after one.
func (l *CorpusLoader) deleteIndex(ctx context.Context) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	if _, err := l.client.typed.Indices.Delete(l.client.indexName).Do(cleanupCtx); err != nil {
		return fmt.Errorf("delete partially loaded index %s: %w", l.client.indexName, err)
	}
	return nil
}

func (l *CorpusLoader) restoreRefresh(ctx context.Context) error {
	index := l.client.indexName
	restored := types.IndexSettings{RefreshInterval: refreshIntervalAfterLoad}
	if _, err := l.client.typed.Indices.PutSettings().Indices(index).Settings(&restored).Do(ctx); err != nil {
		return fmt.Errorf("restore refresh interval of %s: %w", index, err)
	}
	return nil
}

// loadedDocument is an article as the loader writes it, with its vector.
type loadedDocument struct {
	ArticleDocument
	Embedding      []float32 `json:"embedding,omitempty"`
	EmbeddingModel string    `json:"embedding_model,omitempty"`
}

type writeCounts struct {
	articles int64
	vectors  int64
}

// writeDocuments zips Articles with Vectors by position. A broken pairing is
// an error at once; a document that cannot be encoded or is rejected counts as
// a failure, and any failure fails the load once every article was tried.
func (l *CorpusLoader) writeDocuments(ctx context.Context, c storage.Corpus, batchSize int) (writeCounts, error) {
	var nextVector func() (embedding.Vec, error, bool)
	if c.Vectors != nil {
		next, stop := iter.Pull2(c.Vectors)
		defer stop()
		nextVector = next
	}

	var batch bulkBatch
	var counts writeCounts
	failures := &DocumentsFailedError{Index: l.client.indexName}
	flush := func() error {
		if len(batch.ids) == 0 {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := l.sendBatch(ctx, &batch, &counts, failures); err != nil {
			return err
		}
		batch = bulkBatch{}
		return nil
	}

	position := 0
	for article, err := range c.Articles {
		if err != nil {
			return counts, fmt.Errorf("read article %d: %w", position, err)
		}
		doc, err := documentFor(article, c.Language)
		if err != nil {
			return counts, fmt.Errorf("article %d: %w", position, err)
		}
		if nextVector != nil {
			vec, err, ok := nextVector()
			if !ok {
				return counts, fmt.Errorf("vectors end before article %d (%s)", position, article.ID)
			}
			if err != nil {
				return counts, fmt.Errorf("read vector %d: %w", position, err)
			}
			if vec.ID != article.ID {
				return counts, fmt.Errorf("vector %d is for %s, article %d is %s", position, vec.ID, position, article.ID)
			}
			if vec.Model != "" && vec.Model != c.ModelTag {
				return counts, fmt.Errorf("vector %d (%s) is tagged %q, the corpus %q", position, vec.ID, vec.Model, c.ModelTag)
			}
			doc.Embedding, doc.EmbeddingModel = vec.Embedding, c.ModelTag
		}
		position++

		if err := batch.add(article.ID, doc); err != nil {
			failures.add(article.ID, err.Error())
			continue
		}
		if len(batch.ids) == batchSize {
			if err := flush(); err != nil {
				return counts, err
			}
		}
	}
	if err := flush(); err != nil {
		return counts, err
	}
	if nextVector != nil {
		if vec, _, ok := nextVector(); ok {
			return counts, fmt.Errorf("vectors outlast the %d articles, starting with %s", position, vec.ID)
		}
	}
	if failures.Failed > 0 {
		return counts, failures
	}
	return counts, nil
}

func documentFor(article document.Article, corpusLanguage language.Code) (loadedDocument, error) {
	if article.ID == uuid.Nil {
		return loadedDocument{}, errors.New("no id")
	}
	if article.Language != corpusLanguage {
		return loadedDocument{}, fmt.Errorf("%s is in %q, the corpus in %q", article.ID, article.Language, corpusLanguage)
	}
	return loadedDocument{ArticleDocument: newArticleDocument(article)}, nil
}

type bulkBatch struct {
	body       bytes.Buffer
	ids        []uuid.UUID
	hasVectors []bool
}

// add uses the create action, so an id that appears twice in the corpus is a
// rejected document instead of a silent overwrite.
func (b *bulkBatch) add(id uuid.UUID, doc loadedDocument) error {
	source, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encode document: %w", err)
	}
	fmt.Fprintf(&b.body, `{"create":{"_id":%q}}`+"\n", id.String())
	b.body.Write(source)
	b.body.WriteByte('\n')
	b.ids = append(b.ids, id)
	b.hasVectors = append(b.hasVectors, doc.Embedding != nil)
	return nil
}

// sendBatch counts each document from its own item in the bulk response.
func (l *CorpusLoader) sendBatch(ctx context.Context, batch *bulkBatch, counts *writeCounts, failures *DocumentsFailedError) error {
	res, err := l.client.typed.Bulk().Index(l.client.indexName).Raw(&batch.body).Do(ctx)
	if err != nil {
		return fmt.Errorf("bulk write %d articles from %s: %w", len(batch.ids), batch.ids[0], err)
	}
	if len(res.Items) != len(batch.ids) {
		return fmt.Errorf("bulk write of %d articles from %s answered %d items", len(batch.ids), batch.ids[0], len(res.Items))
	}
	for i, item := range res.Items {
		for _, result := range item {
			if result.Error != nil || result.Status >= 300 {
				failures.add(batch.ids[i], itemFailureReason(result))
				continue
			}
			counts.articles++
			if batch.hasVectors[i] {
				counts.vectors++
			}
		}
	}
	return nil
}

func itemFailureReason(item types.ResponseItem) string {
	if item.Error == nil {
		return fmt.Sprintf("status %d", item.Status)
	}
	reason := ""
	if item.Error.Reason != nil {
		reason = *item.Error.Reason
	}
	return fmt.Sprintf("status %d: %s: %s", item.Status, item.Error.Type, reason)
}

var _ storage.CorpusLoader = (*CorpusLoader)(nil)
