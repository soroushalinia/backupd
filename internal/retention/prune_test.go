package retention

import (
	"bytes"
	"context"
	"io"
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
		result = append(result, storage.ObjectInfo{Key: k})
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
	manifestA := `{"sources":[{"files":[{"block_ids":["block1","block2"]}]}]}`
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
