package database

import (
	"context"
	"fmt"
	"io"
)

func init() {
	Register("postgres", newPostgres)
}

type postgresAdapter struct {
	cfg AdapterConfig
}

func newPostgres(cfg AdapterConfig) (Adapter, error) {
	return &postgresAdapter{cfg: cfg}, nil
}

func (p *postgresAdapter) Name() string { return "postgres" }

func (p *postgresAdapter) Dump(ctx context.Context) (io.ReadCloser, error) {
	if p.cfg.DumpTool != "" {
		return p.execDump(ctx)
	}
	return p.nativeDump(ctx)
}

func (p *postgresAdapter) execDump(ctx context.Context) (io.ReadCloser, error) {
	a := &execAdapter{
		name: "postgres",
		cmd:  p.cfg.DumpTool,
		dsn:  p.cfg.DSN,
		parseDSN: func(dsn string) []string {
			parts := parseDSN(dsn)
			if parts == nil {
				return nil
			}
			connStr := fmt.Sprintf("postgres://%s:%s@%s", parts.User, parts.Password, parts.Host)
			if parts.Port != "" {
				connStr += ":" + parts.Port
			}
			if parts.Database != "" {
				connStr += "/" + parts.Database
			}
			return []string{"-d", connStr}
		},
	}
	return a.Dump(ctx)
}

func (p *postgresAdapter) nativeDump(ctx context.Context) (io.ReadCloser, error) {
	return nil, fmt.Errorf("native postgres driver not yet implemented (use dump-tool: pg_dump)")
}
