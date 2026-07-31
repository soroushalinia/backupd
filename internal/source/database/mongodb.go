package database

import (
	"context"
	"fmt"
	"io"
)

func init() {
	Register("mongodb", newMongoDB)
}

type mongoDBAdapter struct {
	cfg AdapterConfig
}

func newMongoDB(cfg AdapterConfig) (Adapter, error) {
	return &mongoDBAdapter{cfg: cfg}, nil
}

func (m *mongoDBAdapter) Name() string { return "mongodb" }

func (m *mongoDBAdapter) Dump(ctx context.Context) (io.ReadCloser, error) {
	if m.cfg.DumpTool == "" {
		return nil, fmt.Errorf("native mongodb driver not implemented (use dump-tool: mongodump)")
	}
	return m.execDump(ctx)
}

// execDump runs `mongodump --uri <dsn> --archive`; the archive flag with no
// path makes mongodump write the dump archive to stdout, which the block
// pipeline consumes as a stream.
func (m *mongoDBAdapter) execDump(ctx context.Context) (io.ReadCloser, error) {
	a := &execAdapter{
		name: "mongodb",
		cmd:  m.cfg.DumpTool,
		dsn:  m.cfg.DSN,
		parseDSN: func(dsn string) []string {
			return []string{"--uri", dsn, "--archive"}
		},
	}
	return a.Dump(ctx)
}
