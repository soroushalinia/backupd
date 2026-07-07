package database

import (
	"context"
	"io"
)

type Adapter interface {
	Type() string
	Dump(ctx context.Context) (io.ReadCloser, error)
	Name() string
}
