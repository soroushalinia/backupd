package retention

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/soroushalinia/backupd/internal/config"
	"github.com/soroushalinia/backupd/internal/state"
	"github.com/soroushalinia/backupd/internal/storage"
)

type mockStorage struct {
	objects map[string][]byte
}

func newMockStorage() *mockStorage {
	return &mockStorage{objects: make(map[string][]byte)}
}

func (m *mockStorage) Upload(ctx context.Context, key string, r io.Reader) error {
	data, _ := io.ReadAll(r)
	m.objects[key] = data
	return nil
}

func (m *mockStorage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	data, ok := m.objects[key]
	if !ok {
		return nil, nil
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *mockStorage) Delete(ctx context.Context, key string) error {
	delete(m.objects, key)
	return nil
}

func (m *mockStorage) List(ctx context.Context, prefix string) ([]storage.ObjectInfo, error) {
	var result []storage.ObjectInfo
	for k := range m.objects {
		if strings.HasPrefix(k, prefix) {
			result = append(result, storage.ObjectInfo{Key: k})
		}
	}
	return result, nil
}

func (m *mockStorage) Exists(ctx context.Context, key string) (bool, error) {
	_, ok := m.objects[key]
	return ok, nil
}

func (m *mockStorage) SetTags(ctx context.Context, key string, tags map[string]string) error {
	return nil
}

func TestPruneDeletesOldSnapshots(t *testing.T) {
	store, err := state.New(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	dest := newMockStorage()
	plan := "test-plan"

	now := time.Now().UTC()
	// record 3 snapshots
	for i, offset := range []int{0, -2, -5} {
		snap := config.Snapshot{
			ID:        string(rune('a' + i)),
			Plan:      plan,
			Timestamp: now.AddDate(0, 0, offset),
			Size:      100,
		}
		store.RecordSnapshot(snap)
		// create manifest in mock storage
		manifestKey := plan + "/snapshots/" + snap.ID + "/manifest.json"
		dest.objects[manifestKey] = []byte(`{}`)
	}

	policy := Policy{KeepLast: 2}
	pruner := NewPruner(store)

	if err := pruner.Prune(context.Background(), plan, policy, dest); err != nil {
		t.Fatal(err)
	}

	// should have deleted oldest snapshot's manifest (c is oldest)
	deletedKey := plan + "/snapshots/c/manifest.json"
	if _, ok := dest.objects[deletedKey]; ok {
		t.Error("expected oldest snapshot manifest to be deleted")
	}

	// newest 2 should still exist
	for _, id := range []string{"a", "b"} {
		key := plan + "/snapshots/" + string(id) + "/manifest.json"
		if _, ok := dest.objects[key]; !ok {
			t.Errorf("expected snapshot %q manifest to remain", string(id))
		}
	}

	// should be reflected in state
	snaps, _ := store.ListSnapshots(plan)
	if len(snaps) != 2 {
		t.Errorf("expected 2 snapshots in state after prune, got %d", len(snaps))
	}
}

func TestPruneDeletesSourceArchives(t *testing.T) {
	store, err := state.New(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	dest := newMockStorage()
	plan := "test-plan"
	now := time.Now().UTC()

	for i, id := range []string{"a", "b"} {
		store.RecordSnapshot(config.Snapshot{
			ID: id, Plan: plan, Timestamp: now.AddDate(0, 0, -i), Size: 100,
		})
		dest.objects[plan+"/snapshots/"+id+"/manifest.json"] = []byte(`{}`)
		// Source archives live under snapshots/<id>/sources/.
		dest.objects[plan+"/snapshots/"+id+"/sources/0.sql"] = []byte("dump")
		dest.objects[plan+"/snapshots/"+id+"/sources/0.sql.enc"] = []byte("enc")
	}

	pruner := NewPruner(store)
	if err := pruner.Prune(context.Background(), plan, Policy{KeepLast: 1}, dest); err != nil {
		t.Fatal(err)
	}

	// Oldest snapshot: manifest and all source archives must be gone.
	for _, key := range []string{
		plan + "/snapshots/b/manifest.json",
		plan + "/snapshots/b/sources/0.sql",
		plan + "/snapshots/b/sources/0.sql.enc",
	} {
		if _, ok := dest.objects[key]; ok {
			t.Errorf("expected %q to be deleted", key)
		}
	}
	// Kept snapshot is untouched.
	if _, ok := dest.objects[plan+"/snapshots/a/sources/0.sql"]; !ok {
		t.Error("expected kept snapshot archive to remain")
	}
}

func TestPruneKeepsDatabaseDumpBlocks(t *testing.T) {
	store, err := state.New(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	dest := newMockStorage()
	plan := "test-plan"
	now := time.Now().UTC()

	// Kept snapshot: a database source whose dump is stored as blocks
	// (source-level "blocks" field) plus a file source.
	manifestA := `{"sources":[
		{"type":"database","key":"x","blocks":[{"id":"dump1"},{"id":"dump2"}]},
		{"type":"file","files":[{"blocks":[{"id":"file1"}]}]}
	]}`
	dest.objects[plan+"/snapshots/a/manifest.json"] = []byte(manifestA)
	store.RecordSnapshot(config.Snapshot{
		ID: "a", Plan: plan, Timestamp: now, Size: 100,
	})

	// Pruned snapshot: only a file block.
	manifestB := `{"sources":[{"type":"file","files":[{"blocks":[{"id":"gone1"}]}]}]}`
	dest.objects[plan+"/snapshots/b/manifest.json"] = []byte(manifestB)
	store.RecordSnapshot(config.Snapshot{
		ID: "b", Plan: plan, Timestamp: now.Add(-24 * time.Hour), Size: 100,
	})

	for _, id := range []string{"dump1", "dump2", "file1", "gone1"} {
		dest.objects[plan+"/blocks/"+id] = []byte("data")
	}

	pruner := NewPruner(store)
	if err := pruner.Prune(context.Background(), plan, Policy{KeepLast: 1}, dest); err != nil {
		t.Fatal(err)
	}

	// dump1/dump2 are referenced by the kept snapshot's database source
	// and must survive; gone1 was only referenced by the pruned snapshot.
	for _, id := range []string{"dump1", "dump2", "file1"} {
		if _, ok := dest.objects[plan+"/blocks/"+id]; !ok {
			t.Errorf("expected block %q to remain (referenced by kept snapshot)", id)
		}
	}
	if _, ok := dest.objects[plan+"/blocks/gone1"]; ok {
		t.Error("expected orphaned block gone1 to be deleted")
	}
}

func TestGCBlocksDeletesOrphans(t *testing.T) {
	store, err := state.New(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	dest := newMockStorage()
	plan := "test-plan"

	dest.objects[plan+"/snapshots/a/manifest.json"] = []byte(`{"sources":[{"type":"file","files":[{"blocks":[{"id":"kept"}]}]}]}`)
	store.RecordSnapshot(config.Snapshot{
		ID: "a", Plan: plan, Timestamp: time.Now(), Size: 100,
	})
	// No state record for the orphaned snapshot; its blocks are unreferenced.
	dest.objects[plan+"/blocks/kept"] = []byte("data")
	dest.objects[plan+"/blocks/orphan"] = []byte("data")

	pruner := NewPruner(store)
	if err := pruner.GCBlocks(context.Background(), plan, dest); err != nil {
		t.Fatal(err)
	}

	if _, ok := dest.objects[plan+"/blocks/orphan"]; ok {
		t.Error("expected orphan block to be deleted")
	}
	if _, ok := dest.objects[plan+"/blocks/kept"]; !ok {
		t.Error("expected referenced block to remain")
	}
}

func TestPruneOrphanBlocks(t *testing.T) {
	store, err := state.New(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	dest := newMockStorage()
	plan := "test-plan"
	now := time.Now().UTC()

	// snapshot a with block references
	manifestA := `{"sources":[{"files":[{"blocks":[{"id":"block1"},{"id":"block2"}]}]}]}`
	manifestKeyA := plan + "/snapshots/a/manifest.json"
	dest.objects[manifestKeyA] = []byte(manifestA)

	// blocks
	dest.objects[plan+"/blocks/block1"] = []byte("data1")
	dest.objects[plan+"/blocks/block2"] = []byte("data2")
	dest.objects[plan+"/blocks/block3"] = []byte("data3")

	snapA := config.Snapshot{
		ID:        "a",
		Plan:      plan,
		Timestamp: now,
		Size:      100,
	}
	store.RecordSnapshot(snapA)

	// snapshot b without block refs (to be pruned)
	snapB := config.Snapshot{
		ID:        "b",
		Plan:      plan,
		Timestamp: now.Add(-24 * time.Hour),
		Size:      100,
	}
	store.RecordSnapshot(snapB)
	dest.objects[plan+"/snapshots/b/manifest.json"] = []byte(`{}`)

	policy := Policy{KeepLast: 1}
	pruner := NewPruner(store)

	if err := pruner.Prune(context.Background(), plan, policy, dest); err != nil {
		t.Fatal(err)
	}

	// block3 is orphaned and should be deleted
	if _, ok := dest.objects[plan+"/blocks/block3"]; ok {
		t.Error("expected orphaned block3 to be deleted")
	}

	// block1 and block2 should remain (referenced by kept snapshot)
	if _, ok := dest.objects[plan+"/blocks/block1"]; !ok {
		t.Error("expected block1 to remain")
	}
	if _, ok := dest.objects[plan+"/blocks/block2"]; !ok {
		t.Error("expected block2 to remain")
	}
}
