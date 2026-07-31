package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/soroushalinia/backupd/internal/crypto"
	"github.com/soroushalinia/backupd/internal/delta"
	"github.com/soroushalinia/backupd/internal/storage"
)

func formatBytes(b int64) string {
	switch {
	case b >= 1024*1024*1024:
		return fmt.Sprintf("%.2f GiB", float64(b)/(1024*1024*1024))
	case b >= 1024*1024:
		return fmt.Sprintf("%.2f MiB", float64(b)/(1024*1024))
	default:
		return fmt.Sprintf("%d bytes", b)
	}
}

type blockRef struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
}

type fileBlockRef struct {
	Path       string      `json:"path"`
	Size       int64       `json:"size"`
	Mode       os.FileMode `json:"mode"`
	LinkTarget string      `json:"link_target,omitempty"`
	Blocks     []blockRef  `json:"blocks"`
	FileHash   string      `json:"file_hash"`
}

type fileManifest struct {
	Files []fileBlockRef `json:"files"`
}

// previousManifest loads the most recent snapshot manifest for a plan, used to
// skip unchanged files during the next backup.
func (e *Engine) previousManifest(ctx context.Context, dest storage.Storage, planName string) (*fileManifest, error) {
	snapshots, err := e.store.ListSnapshots(planName)
	if err != nil {
		return nil, err
	}
	if len(snapshots) == 0 {
		return nil, nil
	}

	manifestKey := fmt.Sprintf("%s/snapshots/%s/manifest.json", planName, snapshots[len(snapshots)-1].ID)
	r, err := dest.Download(ctx, manifestKey)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, nil
	}
	defer r.Close()

	var manifest struct {
		Sources []sourceEntry `json:"sources"`
	}
	if err := json.NewDecoder(r).Decode(&manifest); err != nil {
		return nil, err
	}

	merged := &fileManifest{}
	for _, src := range manifest.Sources {
		if src.Type == "file" {
			merged.Files = append(merged.Files, src.Files...)
		}
	}
	return merged, nil
}

func (e *Engine) backupFilesWithDelta(ctx context.Context, dest storage.Storage, planName, sourceRoot string, exclude []string, encKey []byte) (int64, *fileManifest, error) {
	var total int64
	manifest := &fileManifest{}

	prev, err := e.previousManifest(ctx, dest, planName)
	if err != nil {
		return 0, nil, fmt.Errorf("loading previous manifest: %w", err)
	}
	prevFiles := make(map[string]*fileBlockRef)
	if prev != nil {
		for i := range prev.Files {
			prevFiles[prev.Files[i].Path] = &prev.Files[i]
		}
	}

	log.Printf("  scanning files in %s ...", sourceRoot)

	err = filepath.Walk(sourceRoot, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		if fi.IsDir() {
			if isExcluded(rel, exclude) {
				return filepath.SkipDir
			}
			manifest.Files = append(manifest.Files, fileBlockRef{
				Path: rel,
				Mode: fi.Mode(),
			})
			return nil
		}

		if isExcluded(rel, exclude) {
			return nil
		}

		ref := fileBlockRef{
			Path: rel,
			Size: fi.Size(),
			Mode: fi.Mode(),
		}

		// Symlinks are recorded with their target instead of being
		// followed: following can escape the source root, duplicate
		// content, and fail the whole backup on broken links.
		if fi.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			ref.LinkTarget = target
			manifest.Files = append(manifest.Files, ref)
			return nil
		}

		// Special files (fifos, sockets, devices) cannot be backed up
		// as regular content; skip them rather than hang or fail.
		if !fi.Mode().IsRegular() {
			log.Printf("  skipping special file %s (mode %v)", rel, fi.Mode())
			return nil
		}

		log.Printf("  delta file: %s (%s)", rel, formatBytes(fi.Size()))

		prevRef, hadPrev := prevFiles[rel]

		// Pass 1: stream the file once to compute its hash without
		// buffering it in memory.
		fileHash, err := hashFile(path)
		if err != nil {
			return err
		}
		ref.FileHash = hex.EncodeToString(fileHash)

		// Unchanged file: reuse the block references from the previous
		// manifest without re-encrypting or re-checking storage.
		if hadPrev && prevRef.FileHash == ref.FileHash {
			ref.Blocks = prevRef.Blocks
			manifest.Files = append(manifest.Files, ref)
			return nil
		}

		knownBlocks := make(map[string]bool)
		if hadPrev {
			for _, b := range prevRef.Blocks {
				knownBlocks[b.ID] = true
			}
		}

		// Pass 2: stream again, processing each block (hash, encrypt,
		// upload if new). The second read typically hits the page cache.
		err = processFileBlocks(ctx, dest, planName, path, &ref, knownBlocks, encKey, &total)
		if err != nil {
			return err
		}

		manifest.Files = append(manifest.Files, ref)
		return nil
	})

	if err != nil {
		return 0, nil, err
	}

	log.Printf("  delta complete: %d files, %s new/changed blocks", len(manifest.Files), formatBytes(total))
	return total, manifest, nil
}

// hashFile streams a file through sha256, returning the digest. Memory use is
// bounded by the block size regardless of file size.
func hashFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, fmt.Errorf("hashing %s: %w", path, err)
	}
	return h.Sum(nil), nil
}

// processFileBlocks streams a file in fixed-size blocks, encrypting each
// block and uploading it only when it does not exist in storage yet.
func processFileBlocks(ctx context.Context, dest storage.Storage, planName, path string, ref *fileBlockRef, knownBlocks map[string]bool, encKey []byte, total *int64) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	blockSize := delta.DefaultBlockSize
	buf := make([]byte, blockSize)
	for {
		n, err := io.ReadFull(f, buf)
		if n == 0 && err == io.EOF {
			break
		}
		if n == 0 && err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		block := buf[:n]
		plainHash := sha256.Sum256(block)
		blockID := hex.EncodeToString(plainHash[:])
		stored := block

		if encKey != nil {
			enc, err := crypto.EncryptBlock(encKey, block)
			if err != nil {
				return fmt.Errorf("encrypting block of %s: %w", path, err)
			}
			stored = enc
			idHash := sha256.Sum256(enc)
			blockID = hex.EncodeToString(idHash[:])
		}

		ref.Blocks = append(ref.Blocks, blockRef{ID: blockID, Hash: hex.EncodeToString(plainHash[:])})

		if !knownBlocks[blockID] {
			blockKey := fmt.Sprintf("%s/blocks/%s", planName, blockID)
			exists, err := dest.Exists(ctx, blockKey)
			if err != nil {
				return fmt.Errorf("checking block %s: %w", blockID, err)
			}
			if !exists {
				if err := dest.Upload(ctx, blockKey, bytes.NewReader(stored)); err != nil {
					return fmt.Errorf("uploading block: %w", err)
				}
				*total += int64(len(stored))
			}
		}

		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
	}
	return nil
}

func (e *Engine) restoreFilesWithDelta(ctx context.Context, dest storage.Storage, planName, target string, manifest *fileManifest, encKey []byte) error {
	// Pass 1: recreate directories (including empty ones) with their modes.
	for _, ref := range manifest.Files {
		if ref.Mode&os.ModeDir == 0 {
			continue
		}
		outPath, err := safeJoin(target, ref.Path)
		if err != nil {
			return fmt.Errorf("dir %q: %w", ref.Path, err)
		}
		if err := os.MkdirAll(outPath, ref.Mode.Perm()); err != nil {
			return err
		}
		if err := os.Chmod(outPath, ref.Mode.Perm()); err != nil {
			return err
		}
	}

	// Pass 2: write regular files. This must happen before symlinks are
	// created so a malicious manifest cannot redirect writes through a
	// restored symlink out of the target directory.
	for _, ref := range manifest.Files {
		if ref.Mode&os.ModeDir != 0 || ref.Mode&os.ModeSymlink != 0 {
			continue
		}
		outPath, err := safeJoin(target, ref.Path)
		if err != nil {
			return fmt.Errorf("file %q: %w", ref.Path, err)
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return err
		}

		f, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, ref.Mode)
		if err != nil {
			return err
		}

		for _, block := range ref.Blocks {
			blockKey := fmt.Sprintf("%s/blocks/%s", planName, block.ID)
			r, err := dest.Download(ctx, blockKey)
			if err != nil {
				f.Close()
				return fmt.Errorf("downloading block %s: %w", block.ID, err)
			}
			if r == nil {
				f.Close()
				return fmt.Errorf("block %s not found", block.ID)
			}

			blockData, err := io.ReadAll(r)
			r.Close()
			if err != nil {
				f.Close()
				return err
			}

			plain := blockData
			if encKey != nil {
				plainHash, err := hex.DecodeString(block.Hash)
				if err != nil {
					f.Close()
					return fmt.Errorf("invalid block hash: %w", err)
				}
				plain, err = crypto.DecryptBlock(encKey, plainHash, blockData)
				if err != nil {
					f.Close()
					return fmt.Errorf("decrypting block %s: %w", block.ID, err)
				}
				computed := sha256.Sum256(plain)
				if !bytes.Equal(computed[:], plainHash) {
					f.Close()
					return fmt.Errorf("block %s: hash mismatch (corrupt)", block.ID)
				}
			}

			if _, err := f.Write(plain); err != nil {
				f.Close()
				return err
			}
		}

		if err := f.Close(); err != nil {
			return err
		}
	}

	// Pass 3: recreate symlinks. The targets are restored verbatim; they
	// are never followed during restore.
	for _, ref := range manifest.Files {
		if ref.Mode&os.ModeSymlink == 0 {
			continue
		}
		outPath, err := safeJoin(target, ref.Path)
		if err != nil {
			return fmt.Errorf("symlink %q: %w", ref.Path, err)
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return err
		}
		if err := os.Symlink(ref.LinkTarget, outPath); err != nil {
			return fmt.Errorf("creating symlink %q: %w", ref.Path, err)
		}
	}
	return nil
}

// safeJoin joins rel onto base and verifies the result stays inside base,
// preventing path traversal via manifest-controlled paths.
func safeJoin(base, rel string) (string, error) {
	cleanBase := filepath.Clean(base)
	joined := filepath.Join(cleanBase, rel)
	if joined != cleanBase && !strings.HasPrefix(joined, cleanBase+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes target directory")
	}
	return joined, nil
}

// isExcluded reports whether rel should be skipped. Patterns are matched
// against the full relative path, the file basename (so "*.log" matches
// nested files), and as a plain substring (so "cache" or "cache/" matches
// any directory segment). Trailing slashes are ignored so a pattern like
// "cache/" also matches the directory entry "cache" itself.
func isExcluded(rel string, exclude []string) bool {
	for _, ex := range exclude {
		ex = strings.TrimSuffix(ex, "/")
		if matched, _ := filepath.Match(ex, rel); matched {
			return true
		}
		if matched, _ := filepath.Match(ex, filepath.Base(rel)); matched {
			return true
		}
		if strings.Contains(rel, ex) {
			return true
		}
	}
	return false
}

func newSnapshotID() string {
	return uuid.New().String()
}
