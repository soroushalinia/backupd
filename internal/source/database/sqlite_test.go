package database

import (
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLiteDump(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not available")
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	if out, err := exec.Command("sqlite3", dbPath,
		"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);",
		"INSERT INTO users (name) VALUES ('alice');",
	).CombinedOutput(); err != nil {
		t.Fatalf("creating sqlite db: %v: %s", err, out)
	}

	adapter, err := Get("sqlite", AdapterConfig{
		DSN:      dbPath,
		DumpTool: "sqlite3",
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
	if !strings.Contains(string(data), "INSERT INTO users") {
		t.Errorf("dump does not contain table data:\n%s", data)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("closing dump: %v", err)
	}
}

func TestSQLiteDumpMissingFile(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not available")
	}

	adapter, err := Get("sqlite", AdapterConfig{
		DSN:      filepath.Join(t.TempDir(), "does-not-exist.db"),
		DumpTool: "sqlite3",
	})
	if err != nil {
		t.Fatal(err)
	}

	rc, err := adapter.Dump(context.Background())
	if err == nil {
		rc.Close()
		t.Fatal("expected error dumping a missing database file")
	}
}
