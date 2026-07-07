package storage

import (
	"context"
	"io"
)

type Storage interface {
	Upload(ctx context.Context, key string, r io.Reader) error
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) ([]ObjectInfo, error)
	Exists(ctx context.Context, key string) (bool, error)
}

type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified string
}
