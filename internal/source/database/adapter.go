package database

import (
	"context"
	"fmt"
	"io"
	"strings"
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

type dsnParts struct {
	User     string
	Password string
	Host     string
	Port     string
	Database string
}

func parseDSN(dsn string) *dsnParts {
	raw := dsn
	if idx := strings.Index(raw, "://"); idx >= 0 {
		raw = raw[idx+3:]
	}

	lastAt := strings.LastIndex(raw, "@")
	if lastAt < 0 {
		return nil
	}

	userinfo := raw[:lastAt]
	hostpart := raw[lastAt+1:]

	var p dsnParts

	if colon := strings.Index(userinfo, ":"); colon >= 0 {
		p.User = userinfo[:colon]
		p.Password = userinfo[colon+1:]
	} else {
		p.User = userinfo
	}

	host := hostpart
	if slash := strings.Index(hostpart, "/"); slash >= 0 {
		host = hostpart[:slash]
		p.Database = hostpart[slash+1:]
	}

	if colon := strings.Index(host, ":"); colon >= 0 {
		p.Host = host[:colon]
		p.Port = host[colon+1:]
	} else {
		p.Host = host
	}

	return &p
}
