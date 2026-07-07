package database

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
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
			var args []string
			u, err := url.Parse(dsn)
			if err != nil {
				return nil
			}
			if u.User != nil {
				args = append(args, "-u"+u.User.Username())
				if pass, ok := u.User.Password(); ok {
					args = append(args, "-p"+pass)
				}
			}
			host := u.Host
			if host != "" {
				args = append(args, "-h"+host)
			}
			db := strings.TrimPrefix(u.Path, "/")
			if db != "" {
				args = append(args, db)
			}
			return args
		},
	}
	return a.Dump(ctx)
}

func (m *mysqlAdapter) nativeDump(ctx context.Context) (io.ReadCloser, error) {
	return nil, fmt.Errorf("native mysql driver not yet implemented (use dump-tool: mysqldump)")
}
