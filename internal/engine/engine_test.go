package engine

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/xero/backupd/internal/config"
	"github.com/xero/backupd/internal/state"
	"github.com/xero/backupd/internal/storage"
)

type testStorage struct {
	data map[string][]byte
}

func (s *testStorage) Upload(ctx context.Context, key string, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if s.data == nil {
		s.data = make(map[string][]byte)
	}
	s.data[key] = b
	return nil
}

func (s *testStorage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	b, ok := s.data[key]
	if !ok {
		return nil, nil
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (s *testStorage) Delete(ctx context.Context, key string) error   { return nil }
func (s *testStorage) List(ctx context.Context, prefix string) ([]storage.ObjectInfo, error) {
	return nil, nil
}
func (s *testStorage) Exists(ctx context.Context, key string) (bool, error) {
	_, ok := s.data[key]
	return ok, nil
}

func TestEngineRun(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}

	store, err := state.New(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	eng := New(store)

	plan := config.Plan{
		Name: "test-plan",
		Sources: []config.Source{
			{Type: "file", Path: dir, Exclude: nil},
		},
		Destination: config.Destination{
			Type: "s3", Bucket: "test", Endpoint: "example.com",
		},
		Tags: map[string]string{"env": "test"},
	}

	result, err := eng.Run(context.Background(), plan, &testStorage{})
	if err != nil {
		t.Fatal(err)
	}

	if result.SnapshotID == "" {
		t.Fatal("expected non-empty snapshot ID")
	}
	if result.Size == 0 {
		t.Fatal("expected non-zero size")
	}
	if result.Duration == 0 {
		t.Fatal("expected non-zero duration")
	}

	snap, err := store.LastSnapshot("test-plan")
	if err != nil {
		t.Fatal(err)
	}
	if snap == nil {
		t.Fatal("expected snapshot in state")
	}
	if snap.ID != result.SnapshotID {
		t.Errorf("snapshot ID mismatch: %s vs %s", snap.ID, result.SnapshotID)
	}
}

func TestEngineRunThenRestore(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "data.txt"), []byte("backup me"), 0644); err != nil {
		t.Fatal(err)
	}

	store, err := state.New(filepath.Join(src, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	eng := New(store)
	st := &testStorage{}

	plan := config.Plan{
		Name: "restore-test",
		Sources: []config.Source{
			{Type: "file", Path: src},
		},
		Destination: config.Destination{
			Type: "s3", Bucket: "b", Endpoint: "e",
		},
	}

	result, err := eng.Run(context.Background(), plan, st)
	if err != nil {
		t.Fatal(err)
	}

	dst := t.TempDir()
	if err := eng.Restore(context.Background(), "restore-test", result.SnapshotID, dst, st); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(filepath.Join(dst, "data.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "backup me" {
		t.Errorf("restored content = %q, want %q", string(b), "backup me")
	}
}

func TestEngineRunNoSources(t *testing.T) {
	store, err := state.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	eng := New(store)
	plan := config.Plan{
		Name: "empty",
		Sources: []config.Source{
			{Type: "file", Path: t.TempDir()},
		},
		Destination: config.Destination{
			Type: "s3", Bucket: "b", Endpoint: "e",
		},
	}

	_, err = eng.Run(context.Background(), plan, &testStorage{})
	if err != nil {
		t.Fatal(err)
	}
}
