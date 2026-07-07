package database

import (
	"context"
	"fmt"
	"io"
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

func (s *sqliteAdapter) execDump(ctx context.Context) (io.ReadCloser, error) {
	return nil, fmt.Errorf("sqlite exec adapter not implemented yet")
}
