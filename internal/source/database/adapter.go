package database

import (
	"context"
	"fmt"
	"io"
)

type AdapterConfig struct {
	DSN      string
	DumpTool string
	Adapter  string
}

type Adapter interface {
	Name() string
	Dump(ctx context.Context) (io.ReadCloser, error)
}

var adapters = map[string]func(AdapterConfig) (Adapter, error){}

func Register(name string, fn func(AdapterConfig) (Adapter, error)) {
	adapters[name] = fn
}

func Get(name string, cfg AdapterConfig) (Adapter, error) {
	fn, ok := adapters[name]
	if !ok {
		return nil, fmt.Errorf("unknown database adapter: %q (available: %v)", name, availableAdapters())
	}
	return fn(cfg)
}

func availableAdapters() []string {
	var names []string
	for n := range adapters {
		names = append(names, n)
	}
	return names
}
