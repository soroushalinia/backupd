package database

import (
	"context"
	"fmt"
	"io"
	"os"
)

func init() {
	Register("sqlite", newSQLite)
}

type sqliteAdapter struct {
	cfg AdapterConfig
}

func newSQLite(cfg AdapterConfig) (Adapter, error) {
	return &sqliteAdapter{cfg: cfg}, nil
}

func (s *sqliteAdapter) Name() string { return "sqlite" }

func (s *sqliteAdapter) Dump(ctx context.Context) (io.ReadCloser, error) {
	if s.cfg.DumpTool != "" {
		return s.execDump(ctx)
	}
	return nil, fmt.Errorf("native sqlite driver not yet implemented (use dump-tool: sqlite3)")
}

// execDump runs `sqlite3 <db-file> .dump`; the DSN is the database file
// path. The file is checked up front because sqlite3 would otherwise
// silently create an empty database and back up nothing.
func (s *sqliteAdapter) execDump(ctx context.Context) (io.ReadCloser, error) {
	if _, err := os.Stat(s.cfg.DSN); err != nil {
		return nil, fmt.Errorf("sqlite database file: %w", err)
	}
	a := &execAdapter{
		name: "sqlite",
		cmd:  s.cfg.DumpTool,
		dsn:  s.cfg.DSN,
		parseDSN: func(dsn string) []string {
			return []string{dsn, ".dump"}
		},
	}
	return a.Dump(ctx)
}
