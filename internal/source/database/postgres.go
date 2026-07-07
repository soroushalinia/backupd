package database

import (
	"context"
	"fmt"
	"io"
	"net/url"
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
			var args []string
			u, err := url.Parse(dsn)
			if err != nil {
				return nil
			}
			if u.User != nil {
				if pass, ok := u.User.Password(); ok {
					args = append(args, fmt.Sprintf("postgres://%s:%s@%s%s", u.User.Username(), pass, u.Host, u.Path))
				} else {
					args = append(args, fmt.Sprintf("postgres://%s@%s%s", u.User.Username(), u.Host, u.Path))
				}
			} else {
				args = append(args, dsn)
			}
			return args
		},
	}
	return a.Dump(ctx)
}

func (p *postgresAdapter) nativeDump(ctx context.Context) (io.ReadCloser, error) {
	return nil, fmt.Errorf("native postgres driver not yet implemented (use dump-tool: pg_dump)")
}
