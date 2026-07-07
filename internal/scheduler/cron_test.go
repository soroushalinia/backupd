package scheduler

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/xero/backupd/internal/config"
	"github.com/xero/backupd/internal/state"
)

func TestNewDaemon(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	store, err := state.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := &config.Config{
		Plans: []config.Plan{
			{Name: "test", Schedule: "@every 1h", Destination: config.Destination{
				Endpoint: "http://minio:9000", Bucket: "test",
			}},
			{Name: "nosched", Destination: config.Destination{
				Endpoint: "http://minio:9000", Bucket: "test",
			}},
		},
	}

	d, err := NewDaemon(cfg, store)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	if d == nil {
		t.Fatal("expected non-nil daemon")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// should start and stop cleanly
	if err := d.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestNewDaemonBadSchedule(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	store, err := state.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := &config.Config{
		Plans: []config.Plan{
			{Name: "bad", Schedule: "not-a-valid-schedule", Destination: config.Destination{
				Endpoint: "http://minio:9000", Bucket: "test",
			}},
		},
	}

	_, err = NewDaemon(cfg, store)
	if err == nil {
		t.Fatal("expected error for bad schedule")
	}
}
