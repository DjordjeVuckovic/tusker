// Package migrations embeds each Postgres engine's schema and index
// migrations and applies them in golang-migrate's version-table format, so the
// migrate CLI and the corpus loader agree on what is applied.
package migrations

import (
	"cmp"
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"

	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/jackc/pgx/v5"
)

//go:embed native paradedb tiger
var files embed.FS

const (
	schemaTable  = "schema_migrations"
	indexesTable = "schema_migrations_indexes"
)

// Set is one directory of migrations and the version table that tracks it.
type Set struct {
	files fs.FS
	table string
}

// Schema holds the engine's tables and generated columns.
func Schema(e storage.Engine) (Set, error) { return newSet(e, "schema", schemaTable) }

// Indexes holds the engine's indexes, one migration per index.
func Indexes(e storage.Engine) (Set, error) { return newSet(e, "indexes", indexesTable) }

func newSet(e storage.Engine, dir, table string) (Set, error) {
	if e.Type() != storage.PG {
		return Set{}, fmt.Errorf("engine %q has no Postgres migrations", e)
	}
	sub, err := fs.Sub(files, path.Join(string(e), dir))
	if err != nil {
		return Set{}, fmt.Errorf("open %s migrations of %s: %w", dir, e, err)
	}
	return Set{files: sub, table: table}, nil
}

// Migration is one up file; Name is its file name minus version and suffix.
type Migration struct {
	Version uint64
	Name    string
	file    string
}

// upFile is golang-migrate's file name pattern, restricted to up migrations.
var upFile = regexp.MustCompile(`^([0-9]+)_(.*)\.up\.sql$`)

func (s Set) Migrations() ([]Migration, error) {
	entries, err := fs.ReadDir(s.files, ".")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	var list []Migration
	for _, entry := range entries {
		match := upFile.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		version, err := strconv.ParseUint(match[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("migration %s: %w", entry.Name(), err)
		}
		list = append(list, Migration{Version: version, Name: match[2], file: entry.Name()})
	}
	slices.SortFunc(list, func(a, b Migration) int { return cmp.Compare(a.Version, b.Version) })
	return list, nil
}

// DirtyError reports a version table left dirty by a migration that failed midway.
type DirtyError struct {
	Table   string
	Version uint64
}

func (e *DirtyError) Error() string {
	return fmt.Sprintf("%s is dirty at version %d: a migration failed midway; repair it, then force the version", e.Table, e.Version)
}

// Version returns the last applied version; ok is false when none is.
func (s Set) Version(ctx context.Context, conn *pgx.Conn) (version uint64, ok bool, err error) {
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", s.table).Scan(&exists); err != nil {
		return 0, false, fmt.Errorf("look up %s: %w", s.table, err)
	}
	if !exists {
		return 0, false, nil
	}
	var dirty bool
	err = conn.QueryRow(ctx, "SELECT version, dirty FROM "+s.identifier()+" LIMIT 1").Scan(&version, &dirty)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read %s: %w", s.table, err)
	}
	if dirty {
		return 0, false, &DirtyError{Table: s.table, Version: version}
	}
	return version, true, nil
}

// Pending lists the migrations after the applied version. A database at a
// version the set does not hold was migrated from other files and is refused.
func (s Set) Pending(ctx context.Context, conn *pgx.Conn) ([]Migration, error) {
	all, err := s.Migrations()
	if err != nil {
		return nil, err
	}
	version, ok, err := s.Version(ctx, conn)
	if err != nil || !ok {
		return all, err
	}
	at := slices.IndexFunc(all, func(m Migration) bool { return m.Version == version })
	if at < 0 {
		return nil, fmt.Errorf("%s is at version %d, which no migration in this set has", s.table, version)
	}
	return all[at+1:], nil
}

// Apply runs one migration on conn and records its version. A failed
// migration leaves the version dirty, as golang-migrate does.
func (s Set) Apply(ctx context.Context, conn *pgx.Conn, m Migration) error {
	body, err := fs.ReadFile(s.files, m.file)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", m.file, err)
	}
	if _, err := conn.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+s.identifier()+
		" (version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL)"); err != nil {
		return fmt.Errorf("create %s: %w", s.table, err)
	}
	if err := s.setVersion(ctx, conn, m.Version, true); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, string(body)); err != nil {
		return fmt.Errorf("apply migration %s: %w", m.file, err)
	}
	return s.setVersion(ctx, conn, m.Version, false)
}

func (s Set) Up(ctx context.Context, conn *pgx.Conn) error {
	pending, err := s.Pending(ctx, conn)
	if err != nil {
		return err
	}
	for _, m := range pending {
		if err := s.Apply(ctx, conn, m); err != nil {
			return err
		}
	}
	return nil
}

func (s Set) setVersion(ctx context.Context, conn *pgx.Conn, version uint64, dirty bool) error {
	err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "TRUNCATE "+s.identifier()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO "+s.identifier()+" (version, dirty) VALUES ($1, $2)", int64(version), dirty)
		return err
	})
	if err != nil {
		return fmt.Errorf("record version %d in %s: %w", version, s.table, err)
	}
	return nil
}

func (s Set) identifier() string {
	return pgx.Identifier{s.table}.Sanitize()
}
