package es

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"net/http"
	"strconv"
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
	// taskWaitTimeout stays under DefaultResponseHeaderTimeout.
	taskWaitTimeout     = "20s"
	cleanupTimeout      = 30 * time.Second
	maxFailuresReported = 10
	mergedSegments      = 1
	engineName          = "elasticsearch"
	loadMetaKey         = "load"
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

// LoadCorpus creates the index and fails if it exists, bulk-writes every
// article with its vector, refreshes, force-merges to one segment and stores
// the returned record under the index's _meta.load. Any failure deletes the
// index it created, so a failed load leaves nothing behind.
func (l *CorpusLoader) LoadCorpus(ctx context.Context, c storage.Corpus, opts storage.LoadOptions) (record storage.LoadRecord, err error) {
	index := l.client.indexName
	if index == "" {
		return storage.LoadRecord{}, errors.New("load corpus: no index name configured")
	}
	if err := checkCorpus(c, opts); err != nil {
		return storage.LoadRecord{}, fmt.Errorf("load corpus into %s: %w", index, err)
	}

	startedAt := time.Now().UTC()
	info, err := l.client.typed.Info().Do(ctx)
	if err != nil {
		return storage.LoadRecord{}, fmt.Errorf("read elasticsearch version: %w", err)
	}

	var stages []storage.StageTiming
	timed := func(name string, stage func() error) error {
		began := time.Now()
		if err := stage(); err != nil {
			return err
		}
		seconds := time.Since(began).Seconds()
		stages = append(stages, storage.StageTiming{Name: name, Seconds: seconds})
		slog.Info("corpus load stage finished", "index", index, "stage", name, "seconds", seconds)
		return nil
	}

	if err := timed("create_index", func() error { return l.createIndex(ctx, c.Language) }); err != nil {
		return storage.LoadRecord{}, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, l.deleteIndex(ctx))
		}
	}()

	var written writeCounts
	if err := timed("bulk", func() (err error) {
		written, err = l.writeDocuments(ctx, c, opts.BatchSize)
		return err
	}); err != nil {
		return storage.LoadRecord{}, err
	}
	if err := timed("refresh", func() error { return l.refresh(ctx, written.articles) }); err != nil {
		return storage.LoadRecord{}, err
	}
	if err := timed("force_merge", func() error { return l.forceMerge(ctx) }); err != nil {
		return storage.LoadRecord{}, err
	}

	settings, err := l.declaredSettings(ctx)
	if err != nil {
		return storage.LoadRecord{}, err
	}
	record = storage.LoadRecord{
		Engine:        engineName,
		EngineVersion: info.Version.Int,
		StartedAt:     startedAt,
		FinishedAt:    time.Now().UTC(),
		Canonical:     c.Canonical,
		Embeddings:    c.Embeddings,
		ModelTag:      c.ModelTag,
		Articles:      written.articles,
		Vectors:       written.vectors,
		BatchSize:     opts.BatchSize,
		Stages:        stages,
		Settings:      settings,
	}
	if err := l.writeLoadRecord(ctx, record); err != nil {
		return storage.LoadRecord{}, err
	}
	return record, nil
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

// refresh restores the refresh interval the load turned off, then checks that
// search sees exactly the documents the bulk responses reported as stored.
func (l *CorpusLoader) refresh(ctx context.Context, stored int64) error {
	index := l.client.indexName
	restored := types.IndexSettings{RefreshInterval: refreshIntervalAfterLoad}
	if _, err := l.client.typed.Indices.PutSettings().Indices(index).Settings(&restored).Do(ctx); err != nil {
		return fmt.Errorf("restore refresh interval of %s: %w", index, err)
	}
	if _, err := l.client.typed.Indices.Refresh().Index(index).Do(ctx); err != nil {
		return fmt.Errorf("refresh %s: %w", index, err)
	}
	res, err := l.client.typed.Count().Index(index).Do(ctx)
	if err != nil {
		return fmt.Errorf("count documents in %s: %w", index, err)
	}
	if res.Count != stored {
		return fmt.Errorf("%s holds %d documents after refresh, the bulk responses stored %d", index, res.Count, stored)
	}
	return nil
}

// forceMerge leaves one segment, so kNN searches a single HNSW graph. The merge
// runs as a task, since on a full corpus it outlasts any one response timeout.
func (l *CorpusLoader) forceMerge(ctx context.Context) error {
	index := l.client.indexName
	res, err := l.client.typed.Indices.Forcemerge().Index(index).
		MaxNumSegments(strconv.Itoa(mergedSegments)).
		WaitForCompletion(false).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("force-merge %s: %w", index, err)
	}
	if res.Task == nil {
		return fmt.Errorf("force-merge %s: no task id returned", index)
	}
	if err := l.waitForTask(ctx, *res.Task); err != nil {
		return fmt.Errorf("force-merge %s: %w", index, err)
	}

	segments, err := l.searchSegments(ctx)
	if err != nil {
		return err
	}
	if segments != mergedSegments {
		return fmt.Errorf("force-merge %s left %d segments, want %d", index, segments, mergedSegments)
	}
	return nil
}

// waitForTask waits in rounds of taskWaitTimeout, each answered before the
// client's response timeout; Elasticsearch reports an unfinished round as a
// timeout_exception.
func (l *CorpusLoader) waitForTask(ctx context.Context, taskID string) error {
	for {
		res, err := l.client.typed.Tasks.Get(taskID).WaitForCompletion(true).Timeout(taskWaitTimeout).Do(ctx)
		var esErr *types.ElasticsearchError
		if errors.As(err, &esErr) && esErr.ErrorCause.Type == "timeout_exception" {
			slog.Info("waiting for elasticsearch task", "task", taskID)
			continue
		}
		if err != nil {
			return fmt.Errorf("wait for task %s: %w", taskID, err)
		}
		if res.Error != nil {
			return fmt.Errorf("task %s failed: %s", taskID, res.Error.Type)
		}
		if res.Completed {
			return nil
		}
	}
}

func (l *CorpusLoader) searchSegments(ctx context.Context) (int, error) {
	index := l.client.indexName
	res, err := l.client.typed.Indices.Segments().Index(index).Do(ctx)
	if err != nil {
		return 0, fmt.Errorf("read segments of %s: %w", index, err)
	}
	segments := 0
	for _, shards := range res.Indices[index].Shards {
		for _, shard := range shards {
			for _, segment := range shard.Segments {
				if segment.Search {
					segments++
				}
			}
		}
	}
	return segments, nil
}

// declaredSettings reads back every knob the load declared, as the index
// reports it, so the record shows what was built rather than what was asked.
func (l *CorpusLoader) declaredSettings(ctx context.Context) (map[string]string, error) {
	index := l.client.indexName
	res, err := l.client.typed.Indices.GetSettings().Index(index).FlatSettings(true).Perform(ctx)
	if err != nil {
		return nil, fmt.Errorf("read settings of %s: %w", index, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("read settings of %s: status %d", index, res.StatusCode)
	}
	var byIndex map[string]struct {
		Settings map[string]any `json:"settings"`
	}
	if err := json.NewDecoder(res.Body).Decode(&byIndex); err != nil {
		return nil, fmt.Errorf("parse settings of %s: %w", index, err)
	}

	settings := make(map[string]string)
	for _, key := range []string{"index.number_of_shards", "index.number_of_replicas", "index.max_result_window", "index.refresh_interval"} {
		value, ok := byIndex[index].Settings[key].(string)
		if !ok {
			return nil, fmt.Errorf("settings of %s do not declare %s", index, key)
		}
		settings[key] = value
	}

	mappings, err := l.client.typed.Indices.GetMapping().Index(index).Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("read mapping of %s: %w", index, err)
	}
	fields := searchFieldMappings(mappings[index].Mappings.Properties)
	for _, name := range []string{"title", "description", "content"} {
		settings[name+".analyzer"] = fields[name].Analyzer
	}
	vector := fields["embedding"]
	if vector.VectorIndex == nil {
		return nil, fmt.Errorf("mapping of %s declares no vector index options", index)
	}
	settings["embedding.dims"] = strconv.Itoa(vector.Dims)
	settings["embedding.similarity"] = vector.Similarity
	settings["embedding.index_options.type"] = vector.VectorIndex.Type
	settings["embedding.index_options.m"] = strconv.Itoa(vector.VectorIndex.M)
	settings["embedding.index_options.ef_construction"] = strconv.Itoa(vector.VectorIndex.EfConstruction)

	segments, err := l.searchSegments(ctx)
	if err != nil {
		return nil, err
	}
	settings["segments"] = strconv.Itoa(segments)
	return settings, nil
}

func (l *CorpusLoader) writeLoadRecord(ctx context.Context, record storage.LoadRecord) error {
	index := l.client.indexName
	encoded, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode load record: %w", err)
	}
	if _, err := l.client.typed.Indices.PutMapping(index).Meta_(types.Metadata{loadMetaKey: encoded}).Do(ctx); err != nil {
		return fmt.Errorf("store load record in the _meta of %s: %w", index, err)
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
