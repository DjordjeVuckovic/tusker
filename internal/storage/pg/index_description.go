package pg

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// searchExtensions are the extensions whose version changes what a search
// query computes; utility extensions such as uuid-ossp are left out.
var searchExtensions = []string{"vector", "pg_search", "pg_textsearch", "pg_trgm", "fuzzystrmatch"}

var describedTables = []string{"articles", "article_embeddings"}

// IndexDescription is how a Postgres engine had its corpus indexed, read from
// the live server.
type IndexDescription struct {
	ServerVersion string            `json:"server_version"`
	Extensions    []Extension       `json:"extensions,omitempty"`
	Indexes       []IndexDefinition `json:"indexes,omitempty"`
	// Settings holds the live value of every connection setting the pool
	// applied, keyed by setting name.
	Settings map[string]string `json:"settings,omitempty"`
}

type Extension struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type IndexDefinition struct {
	Table      string   `json:"table"`
	Name       string   `json:"name"`
	Definition string   `json:"definition"`
	Options    []string `json:"reloptions,omitempty"`
}

// DescribeIndexes reads the server version, the search extensions, every
// index on the articles and article_embeddings tables, and the session value
// of each setting the pool applies on connect.
func DescribeIndexes(ctx context.Context, pool *ConnectionPool) (*IndexDescription, error) {
	conn, err := pool.conn.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	var description IndexDescription
	if err := conn.QueryRow(ctx, "SELECT current_setting('server_version')").Scan(&description.ServerVersion); err != nil {
		return nil, fmt.Errorf("read server version: %w", err)
	}
	if description.Extensions, err = readSearchExtensions(ctx, conn.Conn()); err != nil {
		return nil, err
	}
	if description.Indexes, err = readIndexDefinitions(ctx, conn.Conn()); err != nil {
		return nil, err
	}
	if description.Settings, err = readSessionSettings(ctx, conn.Conn(), sortedKeys(pool.settings)); err != nil {
		return nil, err
	}
	return &description, nil
}

func readSearchExtensions(ctx context.Context, conn *pgx.Conn) ([]Extension, error) {
	rows, err := conn.Query(ctx,
		"SELECT extname, extversion FROM pg_extension WHERE extname = ANY($1) ORDER BY extname",
		searchExtensions)
	if err != nil {
		return nil, fmt.Errorf("read extensions: %w", err)
	}
	extensions, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Extension])
	if err != nil {
		return nil, fmt.Errorf("read extensions: %w", err)
	}
	return extensions, nil
}

// readIndexDefinitions resolves the tables through the search path, the same
// way the benchmark queries do.
func readIndexDefinitions(ctx context.Context, conn *pgx.Conn) ([]IndexDefinition, error) {
	rows, err := conn.Query(ctx, `
		SELECT t.relname, i.relname, pg_get_indexdef(i.oid), coalesce(i.reloptions, '{}')
		FROM pg_index x
		JOIN pg_class t ON t.oid = x.indrelid
		JOIN pg_class i ON i.oid = x.indexrelid
		WHERE t.oid IN (SELECT to_regclass(name) FROM unnest($1::text[]) AS name)
		ORDER BY t.relname, i.relname`,
		describedTables)
	if err != nil {
		return nil, fmt.Errorf("read index definitions: %w", err)
	}
	indexes, err := pgx.CollectRows(rows, pgx.RowToStructByPos[IndexDefinition])
	if err != nil {
		return nil, fmt.Errorf("read index definitions: %w", err)
	}
	return indexes, nil
}

func readSessionSettings(ctx context.Context, conn *pgx.Conn, names []string) (map[string]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	settings := make(map[string]string, len(names))
	for _, name := range names {
		var value *string
		if err := conn.QueryRow(ctx, "SELECT current_setting($1, true)", name).Scan(&value); err != nil {
			return nil, fmt.Errorf("read setting %s: %w", name, err)
		}
		if value != nil {
			settings[name] = *value
		}
	}
	return settings, nil
}
