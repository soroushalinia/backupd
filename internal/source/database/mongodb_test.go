package database

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestMongoDBDump(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "mongodump")
	script := `#!/bin/sh
case "$*" in
  "--uri mongodb://user:pass@localhost:27017/testdb --archive")
    printf 'fake mongodump archive\n'
    exit 0 ;;
  *)
    echo "unexpected args: $*" >&2
    exit 1 ;;
esac
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	adapter, err := Get("mongodb", AdapterConfig{
		DSN:      "mongodb://user:pass@localhost:27017/testdb",
		DumpTool: fake,
	})
	if err != nil {
		t.Fatal(err)
	}

	rc, err := adapter.Dump(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "fake mongodump archive\n" {
		t.Errorf("dump = %q, want %q", data, "fake mongodump archive\n")
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("closing dump: %v", err)
	}
}

func TestMongoDBDumpRequiresDumpTool(t *testing.T) {
	adapter, err := Get("mongodb", AdapterConfig{
		DSN: "mongodb://localhost:27017/testdb",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rc, err := adapter.Dump(context.Background()); err == nil {
		rc.Close()
		t.Fatal("expected error without a dump tool")
	}
}
