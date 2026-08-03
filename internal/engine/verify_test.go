package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soroushalinia/backupd/internal/config"
	"github.com/soroushalinia/backupd/internal/state"
)

// runOnceWithEncryption writes one file, takes a snapshot, then corrupts a
// stored block in place so the snapshot no longer matches its manifest.
func corruptBlockOfSnapshot(t *testing.T, encrypt bool) {
	t.Helper()

	src := t.TempDir()
	content := []byte(strings.Repeat("verify me", 4000))
	if err := os.WriteFile(filepath.Join(src, "data.txt"), content, 0644); err != nil {
		t.Fatal(err)
	}

	store, err := state.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	eng := New(store)
	st := &testStorage{data: map[string][]byte{}}

	plan := config.Plan{
		Name: "verify-plan",
		Sources: []config.Source{
			{Type: "file", Path: src},
		},
		Destination: config.Destination{Type: "s3", Bucket: "b", Endpoint: "e"},
	}
	if encrypt {
		plan.Encryption = &config.Encryption{Passphrase: "correct horse battery staple"}
	}

	result, err := eng.Run(context.Background(), plan, st)
	if err != nil {
		t.Fatal(err)
	}

	// The snapshot verifies clean before tampering.
	if err := eng.Verify(context.Background(), plan, result.SnapshotID, st); err != nil {
		t.Fatalf("verify before tamper: %v", err)
	}

	// Flip the first byte of every stored block.
	corrupted := 0
	for key := range st.data {
		if strings.HasPrefix(key, "verify-plan/blocks/") {
			b := st.data[key]
			b[0] ^= 0xFF
			corrupted++
		}
	}
	if corrupted == 0 {
		t.Fatal("no blocks found to corrupt")
	}

	err = eng.Verify(context.Background(), plan, result.SnapshotID, st)
	if err == nil {
		t.Fatal("verify passed on tampered blocks; corruption must be detected")
	}
	if !strings.Contains(err.Error(), "corrupt") {
		t.Errorf("error should mention corruption, got: %v", err)
	}
}

// The hash recorded in the manifest is the only integrity check for
// unencrypted backups; verify must apply it, not just download the blocks.
func TestVerifyDetectsCorruptedUnencryptedBlock(t *testing.T) {
	corruptBlockOfSnapshot(t, false)
}

// Encrypted blocks are authenticated by AES-GCM; verify must still surface
// the failure as corruption.
func TestVerifyDetectsCorruptedEncryptedBlock(t *testing.T) {
	corruptBlockOfSnapshot(t, true)
}

// A verify on a missing block must fail rather than silently pass.
func TestVerifyFailsOnMissingBlock(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "data.txt"), []byte("something"), 0644); err != nil {
		t.Fatal(err)
	}

	store, err := state.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	eng := New(store)
	st := &testStorage{data: map[string][]byte{}}

	plan := config.Plan{
		Name: "verify-missing",
		Sources: []config.Source{
			{Type: "file", Path: src},
		},
		Destination: config.Destination{Type: "s3", Bucket: "b", Endpoint: "e"},
	}

	result, err := eng.Run(context.Background(), plan, st)
	if err != nil {
		t.Fatal(err)
	}

	for key := range st.data {
		if strings.HasPrefix(key, "verify-missing/blocks/") {
			delete(st.data, key)
		}
	}

	if err := eng.Verify(context.Background(), plan, result.SnapshotID, st); err == nil {
		t.Fatal("verify passed with a missing block")
	}
}

// Restore must refuse to write silently corrupted data for unencrypted
// backups too: the block content hash is checked before anything is
// written to the target directory.
func TestRestoreRejectsCorruptedUnencryptedBlock(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "data.txt"), []byte("restore me"), 0644); err != nil {
		t.Fatal(err)
	}

	store, err := state.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	eng := New(store)
	st := &testStorage{data: map[string][]byte{}}

	plan := config.Plan{
		Name: "restore-corrupt",
		Sources: []config.Source{
			{Type: "file", Path: src},
		},
		Destination: config.Destination{Type: "s3", Bucket: "b", Endpoint: "e"},
	}

	result, err := eng.Run(context.Background(), plan, st)
	if err != nil {
		t.Fatal(err)
	}

	for key := range st.data {
		if strings.HasPrefix(key, "restore-corrupt/blocks/") {
			st.data[key][0] ^= 0xFF
		}
	}

	dst := t.TempDir()
	err = eng.Restore(context.Background(), plan, result.SnapshotID, dst, st)
	if err == nil {
		t.Fatal("restore succeeded on corrupted block; must fail")
	}
	if !strings.Contains(err.Error(), "corrupt") {
		t.Errorf("error should mention corruption, got: %v", err)
	}
	entries, _ := os.ReadDir(dst)
	if len(entries) != 0 {
		t.Errorf("restore wrote %d files despite corruption", len(entries))
	}
}
