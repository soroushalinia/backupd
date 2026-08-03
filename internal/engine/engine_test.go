package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soroushalinia/backupd/internal/config"
	"github.com/soroushalinia/backupd/internal/state"
	"github.com/soroushalinia/backupd/internal/storage"
)

type testStorage struct {
	data map[string][]byte
}

const deltaBlockSize = 8192

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

func (s *testStorage) Delete(ctx context.Context, key string) error {
	delete(s.data, key)
	return nil
}
func (s *testStorage) List(ctx context.Context, prefix string) ([]storage.ObjectInfo, error) {
	var result []storage.ObjectInfo
	for k := range s.data {
		if strings.HasPrefix(k, prefix) {
			result = append(result, storage.ObjectInfo{Key: k})
		}
	}
	return result, nil
}
func (s *testStorage) Exists(ctx context.Context, key string) (bool, error) {
	_, ok := s.data[key]
	return ok, nil
}
func (s *testStorage) SetTags(ctx context.Context, key string, tags map[string]string) error {
	return nil
}

func (s *testStorage) UploadMultipart(ctx context.Context, key string, r io.Reader) error {
	return s.Upload(ctx, key, r)
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
	if err := eng.Restore(context.Background(), plan, result.SnapshotID, dst, st); err != nil {
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

func TestEngineRunEncryptedRoundTrip(t *testing.T) {
	src := t.TempDir()
	content := []byte("encrypted backup me " + string(bytes.Repeat([]byte("x"), 100000)))
	if err := os.WriteFile(filepath.Join(src, "data.bin"), content, 0644); err != nil {
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
		Name: "enc-test",
		Sources: []config.Source{
			{Type: "file", Path: src},
		},
		Destination: config.Destination{
			Type: "s3", Bucket: "b", Endpoint: "e",
		},
		Encryption: &config.Encryption{Passphrase: "correct horse battery staple"},
	}

	result, err := eng.Run(context.Background(), plan, st)
	if err != nil {
		t.Fatal(err)
	}

	for key := range st.data {
		if !strings.HasPrefix(key, "enc-test/blocks/") {
			continue
		}
		if bytes.Contains(st.data[key], content) {
			t.Errorf("block %s stored plaintext", key)
		}
	}

	dst := t.TempDir()
	if err := eng.Restore(context.Background(), plan, result.SnapshotID, dst, st); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dst, "data.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("restored content mismatch: got %d bytes, want %d", len(got), len(content))
	}
}

func TestEngineRunIncremental(t *testing.T) {
	src := t.TempDir()
	content := []byte(strings.Repeat("abc123", 10000))
	if err := os.WriteFile(filepath.Join(src, "data.txt"), content, 0644); err != nil {
		t.Fatal(err)
	}

	store, err := state.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	eng := New(store)
	st := &testStorage{}

	plan := config.Plan{
		Name: "inc-test",
		Sources: []config.Source{
			{Type: "file", Path: src},
		},
		Destination: config.Destination{
			Type: "s3", Bucket: "b", Endpoint: "e",
		},
	}

	if _, err := eng.Run(context.Background(), plan, st); err != nil {
		t.Fatal(err)
	}

	blocksAfterFirst := 0
	for key := range st.data {
		if strings.HasPrefix(key, "inc-test/blocks/") {
			blocksAfterFirst++
		}
	}

	second, err := eng.Run(context.Background(), plan, st)
	if err != nil {
		t.Fatal(err)
	}

	if second.SnapshotID != "" {
		t.Fatalf("unchanged run created a snapshot %q; nothing should be recorded", second.SnapshotID)
	}

	blocksAfterSecond := 0
	for key := range st.data {
		if strings.HasPrefix(key, "inc-test/blocks/") {
			blocksAfterSecond++
		}
	}
	if blocksAfterSecond != blocksAfterFirst {
		t.Errorf("unchanged file re-uploaded blocks: before=%d after=%d", blocksAfterFirst, blocksAfterSecond)
	}
	if second.Size != 0 {
		t.Errorf("expected zero uploaded bytes for unchanged file, got %d", second.Size)
	}
}

func TestEngineRunIncrementalEncrypted(t *testing.T) {
	src := t.TempDir()
	content := []byte(strings.Repeat("abc123", 10000))
	if err := os.WriteFile(filepath.Join(src, "data.txt"), content, 0644); err != nil {
		t.Fatal(err)
	}

	store, err := state.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	eng := New(store)
	st := &testStorage{}

	plan := config.Plan{
		Name: "inc-enc-test",
		Sources: []config.Source{
			{Type: "file", Path: src},
		},
		Destination: config.Destination{
			Type: "s3", Bucket: "b", Endpoint: "e",
		},
		Encryption: &config.Encryption{Passphrase: "correct horse battery staple"},
	}

	first, err := eng.Run(context.Background(), plan, st)
	if err != nil {
		t.Fatal(err)
	}

	blocksAfterFirst := 0
	for key := range st.data {
		if strings.HasPrefix(key, "inc-enc-test/blocks/") {
			blocksAfterFirst++
		}
	}

	second, err := eng.Run(context.Background(), plan, st)
	if err != nil {
		t.Fatal(err)
	}
	if second.Size != 0 {
		t.Errorf("expected zero uploaded bytes for unchanged file, got %d", second.Size)
	}

	blocksAfterSecond := 0
	for key := range st.data {
		if strings.HasPrefix(key, "inc-enc-test/blocks/") {
			blocksAfterSecond++
		}
	}
	if blocksAfterSecond != blocksAfterFirst {
		t.Errorf("unchanged file re-uploaded blocks: before=%d after=%d", blocksAfterFirst, blocksAfterSecond)
	}

	// The regression: the second run reused blocks encrypted with the
	// first run's key. Every snapshot must be verifiable and restorable,
	// because later manifests reference blocks from earlier runs. The
	// unchanged second run records no snapshot, so only the first is
	// verified here.
	if second.SnapshotID != "" {
		t.Fatalf("unchanged run created a snapshot %q; nothing should be recorded", second.SnapshotID)
	}
	for _, id := range []string{first.SnapshotID} {
		if err := eng.Verify(context.Background(), plan, id, st); err != nil {
			t.Errorf("verify snapshot %s: %v", id, err)
		}

		dst := t.TempDir()
		if err := eng.Restore(context.Background(), plan, id, dst, st); err != nil {
			t.Fatalf("restore snapshot %s: %v", id, err)
		}
		got, err := os.ReadFile(filepath.Join(dst, "data.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, content) {
			t.Errorf("snapshot %s restored %d bytes, want %d", id, len(got), len(content))
		}
	}

	// A changed file must encrypt with the same key and dedup to a new
	// object, leaving the previous one untouched.
	if err := os.WriteFile(filepath.Join(src, "data.txt"), append(content, []byte("extra")...), 0644); err != nil {
		t.Fatal(err)
	}
	third, err := eng.Run(context.Background(), plan, st)
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Verify(context.Background(), plan, third.SnapshotID, st); err != nil {
		t.Errorf("verify snapshot 3: %v", err)
	}
}

// A mid-file insertion shifts every block boundary, so fixed-size block
// dedup would re-upload most of the file. The rsync-style rolling delta
// must instead match the shifted blocks against the previous version and
// upload only the inserted bytes - and the snapshot must still verify and
// restore to the exact new content.
func TestEngineRunRollingDelta(t *testing.T) {
	src := t.TempDir()
	path := filepath.Join(src, "data.bin")

	content := make([]byte, 60000)
	x := uint64(0x9e3779b97f4a7c15)
	for i := range content {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		content[i] = byte(x)
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	store, err := state.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	eng := New(store)
	st := &testStorage{}

	plan := config.Plan{
		Name: "delta-test",
		Sources: []config.Source{
			{Type: "file", Path: src},
		},
		Destination: config.Destination{Type: "s3", Bucket: "b", Endpoint: "e"},
	}

	first, err := eng.Run(context.Background(), plan, st)
	if err != nil {
		t.Fatal(err)
	}
	blocksAfterFirst := 0
	for key := range st.data {
		if strings.HasPrefix(key, "delta-test/blocks/") {
			blocksAfterFirst++
		}
	}

	// Insert 1000 bytes in the middle: everything after the insertion
	// point shifts by 1000 bytes, so no fixed-size block boundary lines
	// up with the previous version anymore.
	inserted := bytes.Repeat([]byte{0xAB}, 1000)
	shifted := make([]byte, 0, len(content)+len(inserted))
	shifted = append(shifted, content[:30000]...)
	shifted = append(shifted, inserted...)
	shifted = append(shifted, content[30000:]...)
	if err := os.WriteFile(path, shifted, 0644); err != nil {
		t.Fatal(err)
	}

	second, err := eng.Run(context.Background(), plan, st)
	if err != nil {
		t.Fatal(err)
	}

	// A full re-upload of the shifted remainder would be far larger
	// (~36 KiB); the rolling delta matches the shifted blocks against the
	// previous version and uploads only the literal runs (~4 KiB).
	if second.Size > 30000 {
		t.Errorf("rolling delta uploaded %d bytes, want far less than the shifted remainder", second.Size)
	}

	// The new snapshot is self-contained: copied block references point
	// at objects still in storage, so verify and restore work unchanged.
	if err := eng.Verify(context.Background(), plan, second.SnapshotID, st); err != nil {
		t.Errorf("verify snapshot 2: %v", err)
	}
	dst := t.TempDir()
	if err := eng.Restore(context.Background(), plan, second.SnapshotID, dst, st); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "data.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, shifted) {
		t.Fatal("restored file mismatch after rolling delta")
	}

	// Restoring the first snapshot must still work: the delta run must
	// not have invalidated the blocks the first snapshot references.
	if err := eng.Verify(context.Background(), plan, first.SnapshotID, st); err != nil {
		t.Errorf("verify snapshot 1: %v", err)
	}
	blocksAfterSecond := 0
	for key := range st.data {
		if strings.HasPrefix(key, "delta-test/blocks/") {
			blocksAfterSecond++
		}
	}
	if blocksAfterSecond-blocksAfterFirst > 4 {
		t.Errorf("delta run added %d blocks, want only the literal runs", blocksAfterSecond-blocksAfterFirst)
	}
}

func TestEngineDatabaseSourceIncremental(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not available")
	}

	dbPath := filepath.Join(t.TempDir(), "app.db")
	if out, err := exec.Command("sqlite3", dbPath,
		"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);",
		"INSERT INTO users (name) VALUES ('alice');",
	).CombinedOutput(); err != nil {
		t.Fatalf("creating sqlite db: %v: %s", err, out)
	}

	store, err := state.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	eng := New(store)
	st := &testStorage{}

	plan := config.Plan{
		Name: "db-plan",
		Sources: []config.Source{
			{Type: "database", Adapter: "sqlite", DSN: dbPath, DumpTool: "sqlite3"},
		},
		Destination: config.Destination{
			Type: "s3", Bucket: "b", Endpoint: "e",
		},
		Encryption: &config.Encryption{Passphrase: "correct horse battery staple"},
	}

	first, err := eng.Run(context.Background(), plan, st)
	if err != nil {
		t.Fatal(err)
	}

	// Dumps are stored as blocks, not as a single archive object.
	for key := range st.data {
		if strings.Contains(key, "sources/") {
			t.Errorf("database dump stored as archive %q", key)
		}
	}

	// An unchanged database uploads nothing and records no snapshot.
	second, err := eng.Run(context.Background(), plan, st)
	if err != nil {
		t.Fatal(err)
	}
	if second.Size != 0 {
		t.Errorf("expected zero upload for unchanged database, got %d", second.Size)
	}
	if second.SnapshotID != "" {
		t.Errorf("unchanged database created snapshot %q; nothing should be recorded", second.SnapshotID)
	}

	// Every snapshot verifies and restores to the current dump.
	wantDump := sqliteDump(t, dbPath)
	for _, id := range []string{first.SnapshotID} {
		if err := eng.Verify(context.Background(), plan, id, st); err != nil {
			t.Errorf("verify snapshot %s: %v", id, err)
		}
		dst := t.TempDir()
		if err := eng.Restore(context.Background(), plan, id, dst, st); err != nil {
			t.Fatalf("restore snapshot %s: %v", id, err)
		}
		got, err := os.ReadFile(filepath.Join(dst, "0.sql"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, wantDump) {
			t.Errorf("snapshot %s dump mismatch", id)
		}
	}

	// A changed database uploads only new blocks; the new snapshot
	// verifies and restores to the new dump.
	if out, err := exec.Command("sqlite3", dbPath, "INSERT INTO users (name) VALUES ('bob');").CombinedOutput(); err != nil {
		t.Fatalf("updating sqlite db: %v: %s", err, out)
	}
	third, err := eng.Run(context.Background(), plan, st)
	if err != nil {
		t.Fatal(err)
	}
	if third.Size == 0 {
		t.Error("expected non-zero upload for changed database")
	}
	if err := eng.Verify(context.Background(), plan, third.SnapshotID, st); err != nil {
		t.Errorf("verify snapshot 3: %v", err)
	}
	dst := t.TempDir()
	if err := eng.Restore(context.Background(), plan, third.SnapshotID, dst, st); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "0.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, sqliteDump(t, dbPath)) {
		t.Error("snapshot 3 dump mismatch")
	}
}

// A dump tool that dies mid-stream must fail the run: the partial dump
// would otherwise be stored as a truncated snapshot.
func TestDatabaseDumpToolFailureFailsRun(t *testing.T) {
	script := filepath.Join(t.TempDir(), "failing-dump.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'partial dump data'\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	dbFile := filepath.Join(t.TempDir(), "db.sqlite")
	if err := os.WriteFile(dbFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := state.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	eng := New(store)
	st := &testStorage{}

	plan := config.Plan{
		Name: "fail-dump-plan",
		Sources: []config.Source{
			{Type: "database", Adapter: "sqlite", DSN: dbFile, DumpTool: script},
		},
		Destination: config.Destination{Type: "s3", Bucket: "b", Endpoint: "e"},
	}

	if _, err := eng.Run(context.Background(), plan, st); err == nil {
		t.Fatal("expected run to fail when the dump tool exits nonzero")
	}

	for key := range st.data {
		if strings.Contains(key, "manifest.json") {
			t.Errorf("failed run wrote a manifest: %s", key)
		}
	}
}

func sqliteDump(t *testing.T, dbPath string) []byte {
	t.Helper()
	out, err := exec.Command("sqlite3", dbPath, ".dump").Output()
	if err != nil {
		t.Fatalf("dumping sqlite db: %v", err)
	}
	return out
}

func TestSafeJoin(t *testing.T) {
	target := t.TempDir()

	cases := []struct {
		name string
		rel  string
		want string
	}{
		{"plain", "dir/file.txt", target + "/dir/file.txt"},
		{"nested", "a/b/c", target + "/a/b/c"},
		{"dot", "./file.txt", target + "/file.txt"},
		{"dotdot-within", "a/../file.txt", target + "/file.txt"},
	}
	for _, tc := range cases {
		got, err := safeJoin(target, tc.rel)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}

	evil := []string{"../escape.txt", "../../../../etc/passwd", "a/../../../../etc/shadow"}
	for _, rel := range evil {
		if _, err := safeJoin(target, rel); err == nil {
			t.Errorf("expected traversal error for %q", rel)
		}
	}
}

func TestUploadAndRestoreSourceRoundTrip(t *testing.T) {
	st := &testStorage{}
	ctx := context.Background()

	content := bytes.Repeat([]byte("dump-data-"), 100000)
	encKey := []byte("0123456789abcdef0123456789abcdef")

	size, err := uploadAndEncrypt(ctx, st, "plan/snap/sources/0.sql", bytes.NewReader(content), encKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	if size == 0 {
		t.Fatal("expected non-zero encrypted size")
	}

	plain, ok := st.data["plan/snap/sources/0.sql.enc"]
	if !ok {
		t.Fatal("encrypted object not stored")
	}
	if bytes.Contains(plain, content) {
		t.Fatal("stored archive contains plaintext")
	}

	eng := New(nil)
	dst := t.TempDir()
	if err := eng.restoreSource(ctx, st, "plan/snap/sources/0.sql", dst, encKey, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "0.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("restored archive mismatch")
	}
}

type multipartRecordingStorage struct {
	testStorage
	multipartCalls int
}

func (m *multipartRecordingStorage) UploadMultipart(ctx context.Context, key string, r io.Reader) error {
	m.multipartCalls++
	return m.testStorage.UploadMultipart(ctx, key, r)
}

func TestUploadMultipartRoundTrip(t *testing.T) {
	st := &multipartRecordingStorage{}
	ctx := context.Background()

	// Large enough to require several 8 MiB parts if the storage chunked.
	content := bytes.Repeat([]byte("mp-data-"), 2*1024*1024)
	encKey := []byte("0123456789abcdef0123456789abcdef")

	size, err := uploadAndEncrypt(ctx, st, "plan/snap/sources/0.tar", bytes.NewReader(content), encKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len(content)) {
		t.Errorf("size = %d, want %d (plaintext bytes)", size, len(content))
	}
	if st.multipartCalls != 1 {
		t.Errorf("multipart calls = %d, want 1 (spool path used)", st.multipartCalls)
	}

	plain, ok := st.data["plan/snap/sources/0.tar.enc"]
	if !ok {
		t.Fatal("encrypted multipart object not stored")
	}
	if bytes.Contains(plain, content) {
		t.Fatal("stored archive contains plaintext")
	}

	eng := New(nil)
	dst := t.TempDir()
	if err := eng.restoreSource(ctx, st, "plan/snap/sources/0.tar", dst, encKey, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "0.tar"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("restored multipart archive mismatch")
	}
}

func TestEngineLargeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.bin")

	// ~96 MiB of non-repeating content so every block is distinct.
	const size = 96 * 1024 * 1024
	content := make([]byte, size)
	x := uint64(0x9e3779b97f4a7c15)
	for i := range content {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		content[i] = byte(x)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := state.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	eng := New(store)
	st := &testStorage{}

	plan := config.Plan{
		Name: "large-plan",
		Sources: []config.Source{
			{Type: "file", Path: dir},
		},
		Destination: config.Destination{Type: "s3", Bucket: "b", Endpoint: "e"},
	}

	first, err := eng.Run(context.Background(), plan, st)
	if err != nil {
		t.Fatal(err)
	}
	if first.Size < size-8192 {
		t.Errorf("uploaded %d bytes, want ~%d", first.Size, size)
	}

	// An unchanged tree uploads nothing, even for a large file.
	second, err := eng.Run(context.Background(), plan, st)
	if err != nil {
		t.Fatal(err)
	}
	if second.Size != 0 {
		t.Errorf("expected zero upload for unchanged tree, got %d", second.Size)
	}

	if err := eng.Verify(context.Background(), plan, first.SnapshotID, st); err != nil {
		t.Errorf("verify: %v", err)
	}

	dst := t.TempDir()
	if err := eng.Restore(context.Background(), plan, first.SnapshotID, dst, st); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "big.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("restored large file mismatch")
	}
}

// A malicious manifest pointing outside the restore target must fail the
// restore at the engine level, for regular files and hardlink targets.
func TestRestoreRejectsTraversal(t *testing.T) {
	store, err := state.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	block := []byte("some block content")
	blockID := blockIDFor(block)

	eng := New(store)
	st := &testStorage{}
	st.data = map[string][]byte{
		"evil-plan/blocks/" + blockID: block,
	}

	plan := config.Plan{
		Name:        "evil-plan",
		Destination: config.Destination{Type: "s3", Bucket: "b", Endpoint: "e"},
	}

	manifest := map[string]any{
		"sources": []any{
			map[string]any{
				"type": "file",
				"files": []any{
					map[string]any{
						"path": "../evil.txt",
						"size": len(block),
						"mode": 0o644,
						"blocks": []any{
							map[string]any{"id": blockID, "hash": blockID},
						},
					},
				},
			},
		},
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	st.data["evil-plan/snapshots/s1/manifest.json"] = raw

	target := t.TempDir()
	if err := eng.Restore(context.Background(), plan, "s1", target, st); err == nil {
		t.Fatal("expected restore to fail for traversal path")
	}
	if _, err := os.Stat(filepath.Join(target, "evil.txt")); !os.IsNotExist(err) {
		t.Fatal("traversal file was written outside the manifest path")
	}
	if _, err := os.Stat(filepath.Dir(target) + "/evil.txt"); !os.IsNotExist(err) {
		t.Fatal("file escaped the restore target")
	}

	// Hardlink targets are validated too.
	manifest["sources"] = []any{
		map[string]any{
			"type": "file",
			"files": []any{
				map[string]any{
					"path":        "safe.txt",
					"size":        len(block),
					"mode":        0o644,
					"blocks":      []any{map[string]any{"id": blockID, "hash": blockID}},
					"hardlink_of": "",
				},
				map[string]any{
					"path":        "linked.txt",
					"size":        len(block),
					"mode":        0o644,
					"hardlink_of": "../escaped-link.txt",
				},
			},
		},
	}
	raw, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	st.data["evil-plan/snapshots/s2/manifest.json"] = raw

	if err := eng.Restore(context.Background(), plan, "s2", target, st); err == nil {
		t.Fatal("expected restore to fail for traversal hardlink target")
	}
}

// A failed run with a plain file source (no docker required) must not
// record a snapshot, and a later run must succeed and clean up.
func TestFailedRunWithFileSource(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := state.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	eng := New(store)
	st := &testStorage{}

	plan := config.Plan{
		Name: "file-plan",
		Sources: []config.Source{
			{Type: "file", Path: dir},
		},
		Destination: config.Destination{Type: "s3", Bucket: "b", Endpoint: "e"},
	}

	fs := &failStorage{testStorage: *st, failOn: "manifest.json"}
	if _, err := eng.Run(context.Background(), plan, fs); err == nil {
		t.Fatal("expected run to fail")
	}
	last, err := store.LastSnapshot(plan.Name)
	if err != nil {
		t.Fatal(err)
	}
	if last != nil {
		t.Fatal("failed run recorded a snapshot")
	}

	// A fresh run against the same data succeeds.
	fs2 := &failStorage{testStorage: *st, failOn: "never-match"}
	res, err := eng.Run(context.Background(), plan, fs2)
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Verify(context.Background(), plan, res.SnapshotID, fs2); err != nil {
		t.Fatal(err)
	}
}

func blockIDFor(block []byte) string {
	sum := sha256.Sum256(block)
	return hex.EncodeToString(sum[:])
}

func TestRestoreDryRun(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("world!"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := state.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	eng := New(store)
	st := &testStorage{}

	plan := config.Plan{
		Name: "dry-plan",
		Sources: []config.Source{
			{Type: "file", Path: dir},
		},
		Destination: config.Destination{Type: "s3", Bucket: "b", Endpoint: "e"},
	}

	res, err := eng.Run(context.Background(), plan, st)
	if err != nil {
		t.Fatal(err)
	}

	reports, err := eng.RestoreDryRun(context.Background(), plan, res.SnapshotID, st)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || reports[0].Type != "file" {
		t.Fatalf("reports = %+v, want one file source", reports)
	}
	if reports[0].Files != 2 {
		t.Errorf("files = %d, want 2", reports[0].Files)
	}
	if reports[0].Size != 11 {
		t.Errorf("size = %d, want 11", reports[0].Size)
	}

	// The dry run must not write anything.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("source dir changed after dry run: %d entries", len(entries))
	}

	// A missing snapshot reports an error.
	if _, err := eng.RestoreDryRun(context.Background(), plan, "nonexistent", st); err == nil {
		t.Fatal("expected error for missing snapshot")
	}
}

func TestIsExcluded(t *testing.T) {
	cases := []struct {
		name    string
		rel     string
		exclude []string
		want    bool
	}{
		{"top-level glob", "app.log", []string{"*.log"}, true},
		{"nested glob", "var/www/app.log", []string{"*.log"}, true},
		{"deep nested glob", "a/b/c/debug.log", []string{"*.log"}, true},
		{"non-match glob", "var/www/index.html", []string{"*.log"}, false},
		{"dir pattern", "var/www/cache/data.bin", []string{"cache/"}, true},
		{"prefix dir", "cache/data.bin", []string{"cache/"}, true},
		{"full path glob", "tmp/scratch", []string{"tmp/*"}, true},
		{"no exclude", "var/www/index.html", nil, false},
		{"doublestar any depth", "var/www/sub/deep/app.log", []string{"**/*.log"}, true},
		{"doublestar dir tree", "var/www/sub/cache/data.bin", []string{"**/cache/**"}, true},
		{"brace alternation", "var/www/report.csv", []string{"*.{log,csv}"}, true},
		{"brace non-match", "var/www/report.txt", []string{"*.{log,csv}"}, false},
		{"question mark", "var/www/app1.log", []string{"app?.log"}, true},
		{"question mark non-match", "var/www/app12.log", []string{"app?.log"}, false},
		{"substring is not a match", "var/www/cachedir/data.bin", []string{"cache"}, false},
	}
	for _, tc := range cases {
		if got := isExcluded(tc.rel, tc.exclude); got != tc.want {
			t.Errorf("%s: isExcluded(%q, %v) = %v, want %v", tc.name, tc.rel, tc.exclude, got, tc.want)
		}
	}
}

type failStorage struct {
	testStorage
	failOn string
}

func (s *failStorage) Upload(ctx context.Context, key string, r io.Reader) error {
	if strings.Contains(key, s.failOn) {
		return fmt.Errorf("injected failure for %s", key)
	}
	return s.testStorage.Upload(ctx, key, r)
}

func (s *failStorage) UploadMultipart(ctx context.Context, key string, r io.Reader) error {
	return s.Upload(ctx, key, r)
}

func TestEngineFileSourceSymlinksAndEmptyDirs(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "data.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(src, "emptydir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(src, "private"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("data.txt", filepath.Join(src, "link-relative")); err != nil {
		t.Fatal(err)
	}

	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(src, "link-absolute")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("nonexistent", filepath.Join(src, "link-broken")); err != nil {
		t.Fatal(err)
	}

	store, err := state.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	eng := New(store)
	st := &testStorage{}

	plan := config.Plan{
		Name: "symlinks-plan",
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

	target := filepath.Join(t.TempDir(), "restore")
	if err := eng.Restore(context.Background(), plan, result.SnapshotID, target, st); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(target, "emptydir")); err != nil {
		t.Errorf("empty dir not restored: %v", err)
	}
	if mode, err := os.Stat(filepath.Join(target, "private")); err != nil {
		t.Errorf("private dir not restored: %v", err)
	} else if mode.Mode().Perm() != 0700 {
		t.Errorf("private dir mode = %v, want 0700", mode.Mode().Perm())
	}

	for name, want := range map[string]string{
		"link-relative": "data.txt",
		"link-absolute": outsideFile,
		"link-broken":   "nonexistent",
	} {
		got, err := os.Readlink(filepath.Join(target, name))
		if err != nil {
			t.Errorf("symlink %s not restored: %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("symlink %s target = %q, want %q", name, got, want)
		}
	}

	if content, err := os.ReadFile(outsideFile); err != nil {
		t.Errorf("outside file disappeared: %v", err)
	} else if string(content) != "original" {
		t.Errorf("restore wrote through symlink: outside file = %q", content)
	}

	if data, err := os.ReadFile(filepath.Join(target, "data.txt")); err != nil {
		t.Errorf("regular file not restored: %v", err)
	} else if string(data) != "content" {
		t.Errorf("regular file content = %q", data)
	}
}

func TestEngineDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}

	store, err := state.New(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	eng := New(store)
	st := &testStorage{}

	plan := config.Plan{
		Name: "dry-run-plan",
		Sources: []config.Source{
			{Type: "file", Path: dir},
		},
		Destination: config.Destination{
			Type: "s3", Bucket: "b", Endpoint: "e",
		},
		Hooks: &config.Hooks{
			PreBackup: []string{"touch /tmp/backupd-dryrun-hook-ran"},
		},
	}

	result, err := eng.DryRun(context.Background(), plan, st)
	if err != nil {
		t.Fatal(err)
	}
	if result.Size == 0 {
		t.Fatal("expected dry run to report a non-zero upload size")
	}
	if result.SnapshotID != "" {
		t.Errorf("dry run reported snapshot ID %q, want empty", result.SnapshotID)
	}

	if len(st.data) != 0 {
		t.Errorf("dry run wrote %d objects to storage", len(st.data))
	}
	snap, err := store.LastSnapshot("dry-run-plan")
	if err != nil {
		t.Fatal(err)
	}
	if snap != nil {
		t.Errorf("dry run recorded snapshot %q in state", snap.ID)
	}
	if _, err := os.Stat("/tmp/backupd-dryrun-hook-ran"); err == nil {
		t.Error("dry run executed a pre-backup hook")
	}
}

func TestFailedRunCleansUpUploadedSources(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("docker daemon not available")
	}

	store, err := state.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	st := &failStorage{testStorage: testStorage{}, failOn: "sources/1"}
	eng := New(store)

	plan := config.Plan{
		Name: "cleanup-test",
		Sources: []config.Source{
			{Type: "docker", Volume: "cleanup-test-vol-a"},
			{Type: "docker", Volume: "cleanup-test-vol-b"},
		},
		Destination: config.Destination{
			Type: "s3", Bucket: "b", Endpoint: "e",
		},
	}

	if _, err := eng.Run(context.Background(), plan, st); err == nil {
		t.Fatal("expected run to fail")
	}

	prefix := "cleanup-test/snapshots/"
	for key := range st.data {
		if strings.HasPrefix(key, prefix) {
			t.Errorf("failed run left snapshot object %q behind", key)
		}
	}
}

func TestFailedRunGCsOrphanedBlocks(t *testing.T) {
	src := t.TempDir()
	content := bytes.Repeat([]byte("x"), 3*deltaBlockSize)
	if err := os.WriteFile(filepath.Join(src, "data.bin"), content, 0644); err != nil {
		t.Fatal(err)
	}

	store, err := state.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	st := &failStorage{testStorage: testStorage{}, failOn: "manifest.json"}
	eng := New(store)

	plan := config.Plan{
		Name: "gc-fail",
		Sources: []config.Source{
			{Type: "file", Path: src},
		},
		Destination: config.Destination{
			Type: "s3", Bucket: "b", Endpoint: "e",
		},
	}

	if _, err := eng.Run(context.Background(), plan, st); err == nil {
		t.Fatal("expected run to fail")
	}

	// The failed run uploaded blocks before the manifest write aborted;
	// GC must have reclaimed them, leaving nothing behind.
	for key := range st.data {
		t.Errorf("failed run left object %q behind", key)
	}
}

func TestRunWithoutRetentionGCsOrphans(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("some content"), 0644); err != nil {
		t.Fatal(err)
	}

	store, err := state.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	st := &testStorage{data: map[string][]byte{}}
	eng := New(store)

	plan := config.Plan{
		Name: "gc-noretain",
		Sources: []config.Source{
			{Type: "file", Path: src},
		},
		Destination: config.Destination{
			Type: "s3", Bucket: "b", Endpoint: "e",
		},
	}

	// An orphan block left over from an earlier failure.
	st.data["gc-noretain/blocks/orphan1"] = []byte("junk")

	if _, err := eng.Run(context.Background(), plan, st); err != nil {
		t.Fatal(err)
	}

	if _, ok := st.data["gc-noretain/blocks/orphan1"]; ok {
		t.Error("expected orphan block to be gc'd even without retention")
	}
	kept := false
	for key := range st.data {
		if strings.HasPrefix(key, "gc-noretain/blocks/") {
			kept = true
		}
	}
	if !kept {
		t.Error("expected referenced blocks to remain")
	}
}

func TestEngineFileSourceHardlinks(t *testing.T) {
	src := t.TempDir()
	content := []byte("hardlinked content")
	if err := os.WriteFile(filepath.Join(src, "a.txt"), content, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(src, "a.txt"), filepath.Join(src, "b.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(src, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(src, "a.txt"), filepath.Join(src, "sub", "c.txt")); err != nil {
		t.Fatal(err)
	}

	store, err := state.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	eng := New(store)
	st := &testStorage{}

	plan := config.Plan{
		Name: "hardlinks-plan",
		Sources: []config.Source{
			{Type: "file", Path: src},
		},
		Destination: config.Destination{
			Type: "s3", Bucket: "b", Endpoint: "e",
		},
	}

	first, err := eng.Run(context.Background(), plan, st)
	if err != nil {
		t.Fatal(err)
	}

	// An unchanged tree (hardlinks included) uploads nothing and records
	// no snapshot.
	second, err := eng.Run(context.Background(), plan, st)
	if err != nil {
		t.Fatal(err)
	}
	if second.Size != 0 {
		t.Errorf("expected zero upload for unchanged hardlinked tree, got %d", second.Size)
	}
	if second.SnapshotID != "" {
		t.Errorf("unchanged tree created snapshot %q; nothing should be recorded", second.SnapshotID)
	}

	// All snapshots verify.
	for _, id := range []string{first.SnapshotID} {
		if err := eng.Verify(context.Background(), plan, id, st); err != nil {
			t.Errorf("verify snapshot %s: %v", id, err)
		}
	}

	// Restore recreates the hardlinks as real links to the canonical file.
	target := filepath.Join(t.TempDir(), "restore")
	if err := eng.Restore(context.Background(), plan, first.SnapshotID, target, st); err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(target, "a.txt")
	got, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("canonical file content mismatch")
	}
	for _, link := range []string{"b.txt", "sub/c.txt"} {
		p := filepath.Join(target, link)
		fi, err := os.Stat(p)
		if err != nil {
			t.Errorf("hardlink %s not restored: %v", link, err)
			continue
		}
		cfi, err := os.Stat(canonical)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(fi, cfi) {
			t.Errorf("%s is not a hardlink to a.txt", link)
		}
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, content) {
			t.Errorf("%s content mismatch", link)
		}
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
