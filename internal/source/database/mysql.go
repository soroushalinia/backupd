package database

import (
	"context"
	"fmt"
	"io"
)

func init() {
	Register("mysql", newMySQL)
}

type mysqlAdapter struct {
	cfg AdapterConfig
}

func newMySQL(cfg AdapterConfig) (Adapter, error) {
	return &mysqlAdapter{cfg: cfg}, nil
}

func (m *mysqlAdapter) Name() string { return "mysql" }

func (m *mysqlAdapter) Dump(ctx context.Context) (io.ReadCloser, error) {
	if m.cfg.DumpTool != "" {
		return m.execDump(ctx)
	}
	return m.nativeDump(ctx)
}

func (m *mysqlAdapter) execDump(ctx context.Context) (io.ReadCloser, error) {
	a := &execAdapter{
		name: "mysql",
		cmd:  m.cfg.DumpTool,
		dsn:  m.cfg.DSN,
		parseDSN: func(dsn string) []string {
			p := parseDSN(dsn)
			if p == nil {
				return nil
			}
			var args []string
			args = append(args, "-u"+p.User)
			if p.Password != "" {
				args = append(args, "-p"+p.Password)
			}
			args = append(args, "-h"+p.Host)
			if p.Port != "" {
				args = append(args, "-P"+p.Port)
			}
			if p.Database != "" {
				args = append(args, p.Database)
			}
			args = append(args, "--no-tablespaces")
			return args
		},
	}
	return a.Dump(ctx)
}

func (m *mysqlAdapter) nativeDump(ctx context.Context) (io.ReadCloser, error) {
	return nil, fmt.Errorf("native mysql driver not yet implemented (use dump-tool: mysqldump)")
}
