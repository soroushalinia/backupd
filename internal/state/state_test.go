package state

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/xero/backupd/internal/config"
)

func TestStoreSnapshots(t *testing.T) {
	dir := t.TempDir()
	store, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	snap := config.Snapshot{
		ID:        "snap-1",
		Plan:      "test-plan",
		Timestamp: time.Now().UTC(),
		Size:      1234,
	}

	if err := store.RecordSnapshot(snap); err != nil {
		t.Fatal(err)
	}

	snaps, err := store.ListSnapshots("test-plan")
	if err != nil {
		t.Fatal(err)
	}

	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps))
	}

	if snaps[0].ID != "snap-1" {
		t.Errorf("id = %q, want %q", snaps[0].ID, "snap-1")
	}
}

func TestLastSnapshot(t *testing.T) {
	dir := t.TempDir()
	store, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	last, err := store.LastSnapshot("test-plan")
	if err != nil {
		t.Fatal(err)
	}
	if last != nil {
		t.Fatal("expected nil for empty store")
	}

	now := time.Now().UTC()
	snap1 := config.Snapshot{ID: "snap-1", Plan: "test-plan", Timestamp: now.Add(-time.Hour), Size: 100}
	snap2 := config.Snapshot{ID: "snap-2", Plan: "test-plan", Timestamp: now, Size: 200}

	if err := store.RecordSnapshot(snap1); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSnapshot(snap2); err != nil {
		t.Fatal(err)
	}

	last, err = store.LastSnapshot("test-plan")
	if err != nil {
		t.Fatal(err)
	}
	if last.ID != "snap-2" {
		t.Errorf("expected snap-2, got %s", last.ID)
	}
}

func TestPlanIsolation(t *testing.T) {
	dir := t.TempDir()
	store, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.RecordSnapshot(config.Snapshot{ID: "s1", Plan: "plan-a", Timestamp: time.Now(), Size: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSnapshot(config.Snapshot{ID: "s2", Plan: "plan-b", Timestamp: time.Now(), Size: 2}); err != nil {
		t.Fatal(err)
	}

	snaps, _ := store.ListSnapshots("plan-a")
	if len(snaps) != 1 || snaps[0].ID != "s1" {
		t.Errorf("plan-a snapshots = %v", snaps)
	}

	snaps, _ = store.ListSnapshots("plan-b")
	if len(snaps) != 1 || snaps[0].ID != "s2" {
		t.Errorf("plan-b snapshots = %v", snaps)
	}
}
