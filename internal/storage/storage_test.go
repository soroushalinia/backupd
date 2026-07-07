package storage

import (
	"bytes"
	"context"
	"io"
	"testing"
)

type memStorage struct {
	data map[string][]byte
}

func (m *memStorage) Upload(ctx context.Context, key string, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if m.data == nil {
		m.data = make(map[string][]byte)
	}
	m.data[key] = b
	return nil
}

func (m *memStorage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	b, ok := m.data[key]
	if !ok {
		return nil, nil
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memStorage) Delete(ctx context.Context, key string) error {
	delete(m.data, key)
	return nil
}

func (m *memStorage) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	var out []ObjectInfo
	for k, v := range m.data {
		out = append(out, ObjectInfo{Key: k, Size: int64(len(v))})
	}
	return out, nil
}

func (m *memStorage) Exists(ctx context.Context, key string) (bool, error) {
	_, ok := m.data[key]
	return ok, nil
}

func TestMemStorageRoundTrip(t *testing.T) {
	s := &memStorage{}
	ctx := context.Background()

	err := s.Upload(ctx, "test-key", bytes.NewReader([]byte("hello world")))
	if err != nil {
		t.Fatal(err)
	}

	exists, err := s.Exists(ctx, "test-key")
	if err != nil || !exists {
		t.Fatal("expected key to exist")
	}

	r, err := s.Download(ctx, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	r.Close()
	if string(b) != "hello world" {
		t.Errorf("got %q, want %q", string(b), "hello world")
	}

	objects, err := s.List(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 1 {
		t.Errorf("expected 1 object, got %d", len(objects))
	}

	err = s.Delete(ctx, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	exists, _ = s.Exists(ctx, "test-key")
	if exists {
		t.Fatal("expected key to be deleted")
	}
}

func TestMemStorageKeyNotFound(t *testing.T) {
	s := &memStorage{}
	r, err := s.Download(context.Background(), "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if r != nil {
		r.Close()
		t.Fatal("expected nil reader for nonexistent key")
	}
}
