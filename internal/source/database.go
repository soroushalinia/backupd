package source

import (
	"context"
	"fmt"
	"io"

	"github.com/soroushalinia/backupd/internal/source/database"
)

type DatabaseSource struct {
	adapter database.Adapter
	dsn     string
	dbType  string
}

func NewDatabaseSource(dbType, dsn, dumpTool string) (*DatabaseSource, error) {
	cfg := database.AdapterConfig{
		DSN:      dsn,
		DumpTool: dumpTool,
		Adapter:  dbType,
	}
	adapter, err := database.Get(dbType, cfg)
	if err != nil {
		return nil, err
	}
	return &DatabaseSource{adapter: adapter, dsn: dsn, dbType: dbType}, nil
}

func (s *DatabaseSource) Type() string { return "database" }

func (s *DatabaseSource) Name() string {
	return fmt.Sprintf("%s:%s", s.dbType, s.dsn)
}

func (s *DatabaseSource) Capture(ctx context.Context) (io.ReadCloser, error) {
	return s.adapter.Dump(ctx)
}
