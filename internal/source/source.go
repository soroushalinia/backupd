package source

import (
	"context"
	"io"
)

type Source interface {
	Type() string
	Capture(ctx context.Context) (io.ReadCloser, error)
	Name() string
}
