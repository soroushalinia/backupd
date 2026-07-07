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
	if m.cfg.DumpTool != "" {
		return m.execDump(ctx)
	}
	return nil, fmt.Errorf("native mongodb driver not yet implemented (use dump-tool: mongodump)")
}

func (m *mongoDBAdapter) execDump(ctx context.Context) (io.ReadCloser, error) {
	return nil, fmt.Errorf("mongodump exec adapter not implemented yet (use dump-tool: mongodump)")
}
