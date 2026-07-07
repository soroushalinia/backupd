package database

import (
	"testing"
)

func TestRegistryPostgres(t *testing.T) {
	adapter, err := Get("postgres", AdapterConfig{
		DSN:      "postgres://user:pass@localhost:5432/testdb",
		DumpTool: "pg_dump",
	})
	if err != nil {
		t.Fatal(err)
	}
	if adapter.Name() != "postgres" {
		t.Errorf("name = %q, want %q", adapter.Name(), "postgres")
	}
}

func TestRegistryMySQL(t *testing.T) {
	adapter, err := Get("mysql", AdapterConfig{
		DSN:      "mysql://user:pass@localhost:3306/testdb",
		DumpTool: "mysqldump",
	})
	if err != nil {
		t.Fatal(err)
	}
	if adapter.Name() != "mysql" {
		t.Errorf("name = %q, want %q", adapter.Name(), "mysql")
	}
}

func TestRegistryUnknown(t *testing.T) {
	_, err := Get("nonexistent", AdapterConfig{})
	if err == nil {
		t.Fatal("expected error for unknown adapter")
	}
}

func TestRegistryMongoDB(t *testing.T) {
	adapter, err := Get("mongodb", AdapterConfig{
		DSN:      "mongodb://localhost:27017/testdb",
		DumpTool: "mongodump",
	})
	if err != nil {
		t.Fatal(err)
	}
	if adapter.Name() != "mongodb" {
		t.Errorf("name = %q, want %q", adapter.Name(), "mongodb")
	}
}

func TestRegistrySQLite(t *testing.T) {
	adapter, err := Get("sqlite", AdapterConfig{
		DSN:      "/tmp/test.db",
		DumpTool: "sqlite3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if adapter.Name() != "sqlite" {
		t.Errorf("name = %q, want %q", adapter.Name(), "sqlite")
	}
}
