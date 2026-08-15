package pg

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
)

type PoolConfig struct {
	ConnStr            string
	RegisterVecTypes   bool
	ConnectionSettings map[string]string
}
type ConnectionPool struct {
	conn *pgxpool.Pool
}

func NewConnectionPool(ctx context.Context, cfg PoolConfig) (*ConnectionPool, error) {
	config, err := pgxpool.ParseConfig(cfg.ConnStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	config.AfterConnect = afterConnect(cfg.RegisterVecTypes, cfg.ConnectionSettings)

	dbpool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	if err := dbpool.Ping(ctx); err != nil {
		dbpool.Close()
		return nil, fmt.Errorf("failed to ping DB: %w", err)
	}

	return &ConnectionPool{conn: dbpool}, nil
}

func afterConnect(registerVec bool, settings map[string]string) func(ctx context.Context, conn *pgx.Conn) error {
	return func(ctx context.Context, conn *pgx.Conn) error {
		if registerVec {
			err := pgxvec.RegisterTypes(ctx, conn)
			if err != nil {
				return err
			}
		}
		for _, name := range sortedKeys(settings) {
			if !isValidGUCName(name) {
				return fmt.Errorf("invalid session setting name %q", name)
			}
			stmt := fmt.Sprintf("SET %s = %s", name, quoteLiteral(settings[name]))
			if _, err := conn.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("apply session setting %s: %w", name, err)
			}
		}
		return nil
	}
}

func isValidGUCName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}

func quoteLiteral(v string) string {
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (p *ConnectionPool) GetConn() *pgxpool.Pool {
	return p.conn
}

func (p *ConnectionPool) Close() {
	p.conn.Close()
}

func (p *ConnectionPool) Ping(ctx context.Context) error {
	c, err := p.conn.Acquire(ctx)
	if err != nil {
		return err
	}
	defer c.Release()
	return c.Ping(ctx)
}
