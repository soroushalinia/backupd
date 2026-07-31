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
	"syscall"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/google/uuid"
	"github.com/soroushalinia/backupd/internal/crypto"
	"github.com/soroushalinia/backupd/internal/delta"
	"github.com/soroushalinia/backupd/internal/ratelimit"
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
	HardlinkOf string      `json:"hardlink_of,omitempty"`
	Blocks     []blockRef  `json:"blocks"`
	FileHash   string      `json:"file_hash"`
}

type fileManifest struct {
	Files []fileBlockRef `json:"files"`
}

// previousSources loads the most recent snapshot manifest for a plan,
// returning its source entries, or nil when no snapshot exists yet.
func (e *Engine) previousSources(ctx context.Context, dest storage.Storage, planName string) ([]sourceEntry, error) {
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
	return manifest.Sources, nil
}

// previousDumpBlocks returns the block references of database sources from
// the most recent snapshot manifest, used to skip already-uploaded blocks.
func (e *Engine) previousDumpBlocks(ctx context.Context, dest storage.Storage, planName string) ([]blockRef, error) {
	sources, err := e.previousSources(ctx, dest, planName)
	if err != nil || sources == nil {
		return nil, err
	}
	var blocks []blockRef
	for _, src := range sources {
		if src.Type == "database" {
			blocks = append(blocks, src.Blocks...)
		}
	}
	return blocks, nil
}

func (e *Engine) backupFilesWithDelta(ctx context.Context, dest storage.Storage, planName, sourceRoot string, exclude []string, encKey []byte, limiter *ratelimit.Limiter) (int64, *fileManifest, error) {
	var total int64
	manifest := &fileManifest{}

	prev, err := e.previousSources(ctx, dest, planName)
	if err != nil {
		return 0, nil, fmt.Errorf("loading previous manifest: %w", err)
	}
	prevFiles := make(map[string]*fileBlockRef)
	for _, src := range prev {
		if src.Type != "file" {
			continue
		}
		for i := range src.Files {
			prevFiles[src.Files[i].Path] = &src.Files[i]
		}
	}

	// Hardlink detection: the first path seen for an (dev, inode) pair is
	// the canonical one that carries the blocks; later paths record
	// HardlinkOf instead and upload nothing.
	inodes := make(map[string]string)

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

		// Another path to an inode we already recorded: store the link
		// reference only - the canonical entry carries the blocks.
		if id, ok := fileID(fi); ok {
			if canonical, seen := inodes[id]; seen {
				ref.HardlinkOf = canonical
				manifest.Files = append(manifest.Files, ref)
				return nil
			}
			inodes[id] = rel
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
		err = processFileBlocks(ctx, dest, planName, path, &ref, knownBlocks, encKey, limiter, &total)
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

// fileID returns a stable identifier for a file's inode, used to detect
// hardlinks during the walk. Stat_t.Dev and Stat_t.Ino exist on every
// supported platform (linux, darwin, freebsd).
func fileID(fi os.FileInfo) (string, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return "", false
	}
	return fmt.Sprintf("%d:%d", st.Dev, st.Ino), true
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
func processFileBlocks(ctx context.Context, dest storage.Storage, planName, path string, ref *fileBlockRef, knownBlocks map[string]bool, encKey []byte, limiter *ratelimit.Limiter, total *int64) error {
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
				if err := dest.Upload(ctx, blockKey, ratelimit.NewReader(ctx, bytes.NewReader(stored), limiter)); err != nil {
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

// backupDumpBlocks streams a source dump (database) in fixed-size blocks,
// content-addressing and uploading only blocks not already in storage. The
// blocks share the plan's block namespace with file sources, so an
// unchanged database uploads nothing and a changed one uploads only the
// blocks that differ.
func (e *Engine) backupDumpBlocks(ctx context.Context, dest storage.Storage, planName string, r io.Reader, prevBlocks []blockRef, encKey []byte, limiter *ratelimit.Limiter) (int64, []blockRef, error) {
	knownBlocks := make(map[string]bool)
	for _, b := range prevBlocks {
		knownBlocks[b.ID] = true
	}

	var total int64
	var refs []blockRef
	blockSize := delta.DefaultBlockSize
	buf := make([]byte, blockSize)
	for {
		n, err := io.ReadFull(r, buf)
		if n == 0 && err == io.EOF {
			break
		}
		if n == 0 && err != nil {
			return 0, nil, fmt.Errorf("reading dump: %w", err)
		}

		block := buf[:n]
		plainHash := sha256.Sum256(block)
		blockID := hex.EncodeToString(plainHash[:])
		stored := block

		if encKey != nil {
			enc, err := crypto.EncryptBlock(encKey, block)
			if err != nil {
				return 0, nil, fmt.Errorf("encrypting dump block: %w", err)
			}
			stored = enc
			idHash := sha256.Sum256(enc)
			blockID = hex.EncodeToString(idHash[:])
		}

		refs = append(refs, blockRef{ID: blockID, Hash: hex.EncodeToString(plainHash[:])})

		if !knownBlocks[blockID] {
			blockKey := fmt.Sprintf("%s/blocks/%s", planName, blockID)
			exists, err := dest.Exists(ctx, blockKey)
			if err != nil {
				return 0, nil, fmt.Errorf("checking block %s: %w", blockID, err)
			}
			if !exists {
				if err := dest.Upload(ctx, blockKey, ratelimit.NewReader(ctx, bytes.NewReader(stored), limiter)); err != nil {
					return 0, nil, fmt.Errorf("uploading block: %w", err)
				}
				total += int64(len(stored))
			}
		}

		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return 0, nil, fmt.Errorf("reading dump: %w", err)
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
	}
	return total, refs, nil
}

// downloadBlock fetches a block, decrypting and hash-verifying it when the
// plan is encrypted.
func downloadBlock(ctx context.Context, dest storage.Storage, planName string, block blockRef, encKey []byte, limiter *ratelimit.Limiter) ([]byte, error) {
	blockKey := fmt.Sprintf("%s/blocks/%s", planName, block.ID)
	r, err := dest.Download(ctx, blockKey)
	if err != nil {
		return nil, fmt.Errorf("downloading block %s: %w", block.ID, err)
	}
	if r == nil {
		return nil, fmt.Errorf("block %s not found", block.ID)
	}
	r = ratelimit.WrapReadCloser(ctx, r, limiter)
	blockData, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		return nil, fmt.Errorf("reading block %s: %w", block.ID, err)
	}

	plain := blockData
	if encKey != nil {
		plainHash, err := hex.DecodeString(block.Hash)
		if err != nil {
			return nil, fmt.Errorf("invalid block hash: %w", err)
		}
		plain, err = crypto.DecryptBlock(encKey, plainHash, blockData)
		if err != nil {
			return nil, fmt.Errorf("decrypting block %s: %w", block.ID, err)
		}
		computed := sha256.Sum256(plain)
		if !bytes.Equal(computed[:], plainHash) {
			return nil, fmt.Errorf("block %s: hash mismatch (corrupt)", block.ID)
		}
	}
	return plain, nil
}

// restoreDumpBlocks reassembles a dump source from its block references,
// writing the plaintext dump to target/<basename>.
func (e *Engine) restoreDumpBlocks(ctx context.Context, dest storage.Storage, planName, srcKey, target string, blocks []blockRef, encKey []byte, limiter *ratelimit.Limiter) error {
	if err := os.MkdirAll(target, 0755); err != nil {
		return fmt.Errorf("creating target dir: %w", err)
	}
	out := filepath.Join(target, filepath.Base(srcKey))
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	for _, block := range blocks {
		plain, err := downloadBlock(ctx, dest, planName, block, encKey, limiter)
		if err != nil {
			f.Close()
			return err
		}
		if _, err := f.Write(plain); err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}

func (e *Engine) restoreFilesWithDelta(ctx context.Context, dest storage.Storage, planName, target string, manifest *fileManifest, encKey []byte, limiter *ratelimit.Limiter) error {
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
	// restored symlink out of the target directory. Hardlink entries
	// carry no blocks; they are recreated in pass 4.
	for _, ref := range manifest.Files {
		if ref.Mode&os.ModeDir != 0 || ref.Mode&os.ModeSymlink != 0 || ref.HardlinkOf != "" {
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
			plain, err := downloadBlock(ctx, dest, planName, block, encKey, limiter)
			if err != nil {
				f.Close()
				return err
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

	// Pass 4: recreate hardlinks. The canonical entry (HardlinkOf) was
	// written in pass 2; both paths are validated to stay inside the
	// target directory.
	for _, ref := range manifest.Files {
		if ref.HardlinkOf == "" {
			continue
		}
		outPath, err := safeJoin(target, ref.Path)
		if err != nil {
			return fmt.Errorf("hardlink %q: %w", ref.Path, err)
		}
		linkTarget, err := safeJoin(target, ref.HardlinkOf)
		if err != nil {
			return fmt.Errorf("hardlink %q -> %q: %w", ref.Path, ref.HardlinkOf, err)
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return err
		}
		if err := os.Link(linkTarget, outPath); err != nil {
			return fmt.Errorf("creating hardlink %q: %w", ref.Path, err)
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

// isExcluded reports whether rel should be skipped. Patterns are doublestar
// globs (gitignore-style: `**`, `{a,b}`, `?`) matched against the full
// relative path, the file basename (so `*.log` matches nested files), and
// the relative path prefixed with `**/` (so a bare `cache` or `cache/`
// matches any path segment). Trailing slashes are ignored so a pattern like
// `cache/` also matches the directory entry `cache` itself.
func isExcluded(rel string, exclude []string) bool {
	for _, ex := range exclude {
		ex = strings.TrimSuffix(ex, "/")
		if matched, _ := doublestar.Match(ex, rel); matched {
			return true
		}
		if matched, _ := doublestar.Match(ex, filepath.Base(rel)); matched {
			return true
		}
		if matched, _ := doublestar.Match("**/"+ex, rel); matched {
			return true
		}
		// A directory pattern also excludes everything beneath the
		// directory, as a backstop to the walker's SkipDir.
		if matched, _ := doublestar.Match("**/"+ex+"/**", rel); matched {
			return true
		}
	}
	return false
}

func newSnapshotID() string {
	return uuid.New().String()
}
