package pg

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"strings"
	"time"

	"github.com/DjordjeVuckovic/tusker/db/migrations"
	"github.com/DjordjeVuckovic/tusker/internal/embedding"
	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/DjordjeVuckovic/tusker/internal/types/language"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pgvector/pgvector-go"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
)

var _ storage.CorpusLoader = (*CorpusLoader)(nil)

// CorpusLoader builds one Postgres engine's corpus: schema migrations, COPY
// of articles and vectors, then each index migration, timed one by one.
type CorpusLoader struct {
	pool    *ConnectionPool
	engine  storage.Engine
	schema  migrations.Set
	indexes migrations.Set
}

// NewCorpusLoader loads into the database pool connects to, which must run
// engine. The pool must not register the vector types, since the extension
// may not exist until the schema migrations run.
func NewCorpusLoader(pool *ConnectionPool, engine storage.Engine) (*CorpusLoader, error) {
	schema, err := migrations.Schema(engine)
	if err != nil {
		return nil, err
	}
	indexes, err := migrations.Indexes(engine)
	if err != nil {
		return nil, err
	}
	return &CorpusLoader{pool: pool, engine: engine, schema: schema, indexes: indexes}, nil
}

// LoadCorpus refuses a database that already holds a corpus or its indexes:
// the caller drops the schema first, so a load never lands on stale state.
func (l *CorpusLoader) LoadCorpus(ctx context.Context, c storage.Corpus, opts storage.LoadOptions) (storage.LoadRecord, error) {
	if err := validateCorpus(c, opts); err != nil {
		return storage.LoadRecord{}, err
	}
	record := storage.LoadRecord{
		Engine:     string(l.engine),
		StartedAt:  now(),
		Canonical:  c.Canonical,
		Embeddings: c.Embeddings,
		ModelTag:   c.ModelTag,
		BatchSize:  opts.BatchSize,
		Stages:     []storage.StageTiming{},
		Settings:   map[string]string{},
	}

	// One session runs the whole load, so the settings an index migration
	// declares can be read back from it; closing it keeps them out of the pool.
	pooled, err := l.pool.conn.Acquire(ctx)
	if err != nil {
		return storage.LoadRecord{}, fmt.Errorf("acquire connection: %w", err)
	}
	conn := pooled.Hijack()
	defer conn.Close(context.WithoutCancel(ctx))

	if err := lockLoad(ctx, conn); err != nil {
		return storage.LoadRecord{}, err
	}
	if err := timed(&record, "schema", func() error { return l.schema.Up(ctx, conn) }); err != nil {
		return storage.LoadRecord{}, err
	}
	if err := l.refuseLoaded(ctx, conn); err != nil {
		return storage.LoadRecord{}, err
	}
	if err := pgxvec.RegisterTypes(ctx, conn); err != nil {
		return storage.LoadRecord{}, fmt.Errorf("register vector types: %w", err)
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return storage.LoadRecord{}, fmt.Errorf("begin load: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var ids []uuid.UUID
	err = timed(&record, "copy_articles", func() error {
		ids, err = copyArticles(ctx, tx, c, opts.BatchSize)
		return err
	})
	if err != nil {
		return storage.LoadRecord{}, err
	}
	record.Articles = int64(len(ids))

	if c.Vectors != nil {
		err = timed(&record, "copy_embeddings", func() error {
			record.Vectors, err = copyVectors(ctx, tx, c, ids, opts.BatchSize)
			return err
		})
		if err != nil {
			return storage.LoadRecord{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return storage.LoadRecord{}, fmt.Errorf("commit load: %w", err)
	}

	if !opts.SkipIndex {
		if err := l.buildIndexes(ctx, conn, &record); err != nil {
			return storage.LoadRecord{}, err
		}
	}
	err = timed(&record, "vacuum_analyze", func() error {
		_, err := conn.Exec(ctx, "VACUUM (ANALYZE) articles, article_embeddings")
		return err
	})
	if err != nil {
		return storage.LoadRecord{}, fmt.Errorf("vacuum analyze: %w", err)
	}
	if record.EngineVersion, err = engineVersion(ctx, conn); err != nil {
		return storage.LoadRecord{}, err
	}

	record.FinishedAt = now()
	// Without its indexes the corpus is not the one a bench run measures, so
	// it gets no load record.
	if opts.SkipIndex {
		return record, nil
	}
	if _, err := conn.Exec(ctx, "INSERT INTO corpus_load (finished_at, record) VALUES ($1, $2)", record.FinishedAt, record); err != nil {
		return storage.LoadRecord{}, fmt.Errorf("record the load in corpus_load: %w", err)
	}
	return record, nil
}

// engineVersion is the server version followed by each search extension's.
func engineVersion(ctx context.Context, conn *pgx.Conn) (string, error) {
	var version strings.Builder
	var server string
	if err := conn.QueryRow(ctx, "SELECT current_setting('server_version')").Scan(&server); err != nil {
		return "", fmt.Errorf("read server version: %w", err)
	}
	version.WriteString("PostgreSQL " + server)
	extensions, err := readSearchExtensions(ctx, conn)
	if err != nil {
		return "", err
	}
	for _, e := range extensions {
		fmt.Fprintf(&version, ", %s %s", e.Name, e.Version)
	}
	return version.String(), nil
}

func validateCorpus(c storage.Corpus, opts storage.LoadOptions) error {
	switch {
	case c.Language != language.English:
		return fmt.Errorf("corpus language %q: the Postgres schemas analyse only %q", c.Language, language.English)
	case c.Articles == nil:
		return errors.New("corpus has no articles")
	case (c.Vectors == nil) != (c.Embeddings == nil):
		return errors.New("corpus vectors and their embeddings package must come together")
	case c.Vectors != nil && c.ModelTag == "":
		return errors.New("corpus vectors carry no model tag")
	case opts.BatchSize <= 0:
		return fmt.Errorf("batch size %d: want at least 1", opts.BatchSize)
	}
	return nil
}

// loadLockKey is the advisory lock that keeps two loads off one database.
const loadLockKey = 0x7475736b6572 // "tusker"

func lockLoad(ctx context.Context, conn *pgx.Conn) error {
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", int64(loadLockKey)).Scan(&locked); err != nil {
		return fmt.Errorf("lock the database for loading: %w", err)
	}
	if !locked {
		return errors.New("another corpus load is running on this database")
	}
	return nil
}

func (l *CorpusLoader) refuseLoaded(ctx context.Context, conn *pgx.Conn) error {
	version, built, err := l.indexes.Version(ctx, conn)
	if err != nil {
		return err
	}
	if built {
		return fmt.Errorf("the indexes are already built (version %d); drop them and the schema before loading", version)
	}
	var loaded bool
	err = conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM articles) OR EXISTS (SELECT 1 FROM corpus_load)").Scan(&loaded)
	if err != nil {
		return fmt.Errorf("check for an existing corpus: %w", err)
	}
	if loaded {
		return errors.New("the database already holds a corpus; drop its schema before loading another")
	}
	return nil
}

// indexBuildSettings are the session settings every index migration declares.
var indexBuildSettings = []string{"maintenance_work_mem", "max_parallel_maintenance_workers"}

// buildIndexes refuses index migrations that disagree on a build setting,
// since the record holds one value per setting.
func (l *CorpusLoader) buildIndexes(ctx context.Context, conn *pgx.Conn, record *storage.LoadRecord) error {
	pending, err := l.indexes.Pending(ctx, conn)
	if err != nil {
		return err
	}
	for _, m := range pending {
		if err := timed(record, m.Name, func() error { return l.indexes.Apply(ctx, conn, m) }); err != nil {
			return err
		}
		for _, name := range indexBuildSettings {
			var value string
			if err := conn.QueryRow(ctx, "SELECT current_setting($1)", name).Scan(&value); err != nil {
				return fmt.Errorf("read setting %s: %w", name, err)
			}
			if previous, seen := record.Settings[name]; seen && previous != value {
				return fmt.Errorf("index migration %s builds with %s = %s, earlier ones with %s", m.Name, name, value, previous)
			}
			record.Settings[name] = value
		}
	}
	return nil
}

// now is microsecond UTC, the precision timestamptz keeps, so a record read
// back from corpus_load equals the one returned.
func now() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

func timed(record *storage.LoadRecord, stage string, run func() error) error {
	start := time.Now()
	if err := run(); err != nil {
		return err
	}
	record.Stages = append(record.Stages, storage.StageTiming{Name: stage, Seconds: time.Since(start).Seconds()})
	return nil
}

// copyArticles returns the ids in corpus order, which the vectors must follow.
func copyArticles(ctx context.Context, tx pgx.Tx, c storage.Corpus, batchSize int) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	toRow := func(a document.Article) ([]any, error) {
		if a.ID == uuid.Nil {
			return nil, fmt.Errorf("article %d has no id", len(ids))
		}
		if a.Language != c.Language {
			return nil, fmt.Errorf("article %s is in %q, the corpus in %q", a.ID, a.Language, c.Language)
		}
		ids = append(ids, a.ID)
		return insertRow(a)
	}
	plan := copyPlan[document.Article]{Table: "articles", Columns: ArticleColumns, Items: c.Articles, ToRow: toRow, BatchSize: batchSize}
	if err := copyBatches(ctx, tx, plan); err != nil {
		return nil, fmt.Errorf("copy articles: %w", err)
	}
	return ids, nil
}

// copyVectors refuses a vector out of step with the articles, which also
// refuses one whose article is not in the corpus.
func copyVectors(ctx context.Context, tx pgx.Tx, c storage.Corpus, ids []uuid.UUID, batchSize int) (int64, error) {
	var n int64
	toRow := func(v embedding.Vec) ([]any, error) {
		if n >= int64(len(ids)) {
			return nil, fmt.Errorf("vector %d for article %s is past the last of %d articles", n, v.ID, len(ids))
		}
		if v.ID != ids[n] {
			return nil, fmt.Errorf("vector %d is for article %s, article %d is %s", n, v.ID, n, ids[n])
		}
		n++
		return []any{v.ID, c.ModelTag, pgvector.NewVector(v.Embedding)}, nil
	}
	plan := copyPlan[embedding.Vec]{
		Table:     "article_embeddings",
		Columns:   []string{"article_id", "model_name", "embedding"},
		Items:     c.Vectors,
		ToRow:     toRow,
		BatchSize: batchSize,
	}
	if err := copyBatches(ctx, tx, plan); err != nil {
		return 0, fmt.Errorf("copy embeddings: %w", err)
	}
	return n, nil
}

type copyPlan[T any] struct {
	Table     string
	Columns   []string
	Items     iter.Seq2[T, error]
	ToRow     func(T) ([]any, error)
	BatchSize int
}

func copyBatches[T any](ctx context.Context, tx pgx.Tx, plan copyPlan[T]) error {
	next, stop := iter.Pull2(plan.Items)
	defer stop()
	src := &batchSource[T]{next: next, toRow: plan.ToRow}
	for !src.exhausted {
		src.left = plan.BatchSize
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{plan.Table}, plan.Columns, src); err != nil {
			return err
		}
	}
	return nil
}

// batchSource is a pgx.CopyFromSource that ends after left rows, so one
// iterator feeds consecutive COPY statements.
type batchSource[T any] struct {
	next      func() (T, error, bool)
	toRow     func(T) ([]any, error)
	left      int
	exhausted bool
	row       []any
	err       error
}

func (s *batchSource[T]) Next() bool {
	if s.left == 0 || s.exhausted || s.err != nil {
		return false
	}
	item, err, ok := s.next()
	if !ok {
		s.exhausted = true
		return false
	}
	if err == nil {
		s.row, err = s.toRow(item)
	}
	if err != nil {
		s.err = err
		return false
	}
	s.left--
	return true
}

func (s *batchSource[T]) Values() ([]any, error) { return s.row, nil }

func (s *batchSource[T]) Err() error { return s.err }
