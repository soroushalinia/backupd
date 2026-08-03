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

// previousDumpBlocks returns the block references of the database source at
// the same plan position from the most recent snapshot manifest. They are
// the base for the rsync-style delta of the new dump: copy operations
// reference them by index, so they must belong to the same dump, not just
// the same plan. A source that did not exist before (or changed position)
// has no base and falls back to a full block upload.
func (e *Engine) previousDumpBlocks(ctx context.Context, dest storage.Storage, planName string, srcIndex int) ([]blockRef, error) {
	sources, err := e.previousSources(ctx, dest, planName)
	if err != nil || sources == nil {
		return nil, err
	}
	base := fmt.Sprintf("/sources/%d.sql", srcIndex)
	for _, src := range sources {
		if src.Type == "database" && strings.HasSuffix(src.Key, base) {
			return src.Blocks, nil
		}
	}
	return nil, nil
}

func (e *Engine) backupFilesWithDelta(ctx context.Context, dest storage.Storage, planName, sourceRoot string, exclude []string, encKey []byte, limiter *ratelimit.Limiter) (int64, *fileManifest, bool, error) {
	var total int64
	manifest := &fileManifest{}

	prev, err := e.previousSources(ctx, dest, planName)
	if err != nil {
		return 0, nil, false, fmt.Errorf("loading previous manifest: %w", err)
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
			log.Printf("  unchanged: %s", rel)
			return nil
		}

		log.Printf("  changed: %s (%s)", rel, formatBytes(fi.Size()))

		// Pass 2: stream again. When a previous version exists, diff the
		// file against the previous version's blocks with the rsync-style
		// rolling checksum: matching ranges become references to blocks
		// already in storage, and only the non-matching literals are
		// uploaded - so even a file shifted by an insertion in the middle
		// uploads only the new bytes. First-time files go through the plain
		// block pipeline. The second read typically hits the page cache.
		before := total
		if hadPrev && len(prevRef.Blocks) > 0 {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			ref.Blocks, err = e.backupStreamWithRollingDelta(ctx, dest, planName, f, prevRef.Blocks, encKey, limiter, &total)
			f.Close()
			if err != nil {
				return err
			}
		} else if err := processFileBlocks(ctx, dest, planName, path, &ref, encKey, limiter, &total); err != nil {
			return err
		}

		log.Printf("  uploaded %s for %s (%d blocks)", formatBytes(total-before), rel, len(ref.Blocks))

		manifest.Files = append(manifest.Files, ref)
		return nil
	})

	if err != nil {
		return 0, nil, false, err
	}

	log.Printf("  delta complete: %d files, %s new/changed blocks", len(manifest.Files), formatBytes(total))
	return total, manifest, fileManifestChanged(prevFiles, manifest), nil
}

// fileManifestChanged reports whether the current walk produced a file list
// different from the previous snapshot's: new paths, deleted paths, or
// paths whose size, mode, hash, link target, or hardlink relation changed.
// A new empty file or a deletion both count as changes even though nothing
// is uploaded, so the snapshot that records them is not skipped.
func fileManifestChanged(prev map[string]*fileBlockRef, cur *fileManifest) bool {
	seen := make(map[string]struct{}, len(cur.Files))
	for i := range cur.Files {
		f := &cur.Files[i]
		seen[f.Path] = struct{}{}
		p, ok := prev[f.Path]
		if !ok {
			return true
		}
		if !sameFileRef(p, f) {
			return true
		}
	}
	for path := range prev {
		if _, ok := seen[path]; !ok {
			return true
		}
	}
	return false
}

func sameFileRef(a, b *fileBlockRef) bool {
	return a.Size == b.Size &&
		a.Mode == b.Mode &&
		a.FileHash == b.FileHash &&
		a.LinkTarget == b.LinkTarget &&
		a.HardlinkOf == b.HardlinkOf
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
func processFileBlocks(ctx context.Context, dest storage.Storage, planName, path string, ref *fileBlockRef, encKey []byte, limiter *ratelimit.Limiter, total *int64) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	buf := make([]byte, delta.DefaultBlockSize)
	for {
		n, readErr := io.ReadFull(f, buf)
		if n == 0 && readErr == io.EOF {
			break
		}
		if n == 0 && readErr != nil {
			return fmt.Errorf("reading %s: %w", path, readErr)
		}

		block, err := uploadBlockIfNew(ctx, dest, planName, buf[:n], encKey, limiter, total)
		if err != nil {
			return fmt.Errorf("block of %s: %w", path, err)
		}
		ref.Blocks = append(ref.Blocks, block)

		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("reading %s: %w", path, readErr)
		}
	}
	return nil
}

// uploadBlockIfNew encrypts a plaintext block when the plan is encrypted,
// then uploads it unless a block with the same content-addressed id already
// exists in storage. The returned block reference is what manifests record;
// the uploaded byte count is added to total.
func uploadBlockIfNew(ctx context.Context, dest storage.Storage, planName string, plain []byte, encKey []byte, limiter *ratelimit.Limiter, total *int64) (blockRef, error) {
	plainHash := sha256.Sum256(plain)
	blockID := hex.EncodeToString(plainHash[:])
	stored := plain

	if encKey != nil {
		enc, err := crypto.EncryptBlock(encKey, plain)
		if err != nil {
			return blockRef{}, fmt.Errorf("encrypting block: %w", err)
		}
		stored = enc
		idHash := sha256.Sum256(enc)
		blockID = hex.EncodeToString(idHash[:])
	}

	ref := blockRef{ID: blockID, Hash: hex.EncodeToString(plainHash[:])}

	blockKey := fmt.Sprintf("%s/blocks/%s", planName, blockID)
	exists, err := dest.Exists(ctx, blockKey)
	if err != nil {
		return blockRef{}, fmt.Errorf("checking block %s: %w", blockID, err)
	}
	if !exists {
		if err := dest.Upload(ctx, blockKey, ratelimit.NewReader(ctx, bytes.NewReader(stored), limiter)); err != nil {
			return blockRef{}, fmt.Errorf("uploading block: %w", err)
		}
		*total += int64(len(stored))
	}
	return ref, nil
}

// backupStreamWithRollingDelta backs up a stream against its previous
// version with the rsync-style rolling-checksum delta. The previous
// version's blocks are downloaded once as the base and signed; the new
// stream is then diffed against the signature. Ranges that match are
// recorded as references to blocks already in storage (nothing uploaded),
// and only the non-matching literal ranges are split into blocks and
// uploaded. The result is a plain block reference list, so restore and
// verify work exactly as for a full backup.
func (e *Engine) backupStreamWithRollingDelta(ctx context.Context, dest storage.Storage, planName string, r io.Reader, prevBlocks []blockRef, encKey []byte, limiter *ratelimit.Limiter, total *int64) ([]blockRef, error) {
	sig, err := delta.Sign(&blockReader{
		ctx:     ctx,
		dest:    dest,
		plan:    planName,
		blocks:  prevBlocks,
		encKey:  encKey,
		limiter: limiter,
	}, delta.DefaultBlockSize)
	if err != nil {
		return nil, fmt.Errorf("signing previous version: %w", err)
	}

	ops, err := delta.Diff(sig, r)
	if err != nil {
		return nil, fmt.Errorf("computing delta: %w", err)
	}

	var refs []blockRef
	for _, op := range ops {
		if op.Copy {
			if op.Index < 0 || op.Index >= len(prevBlocks) {
				return nil, fmt.Errorf("delta references block %d out of range", op.Index)
			}
			refs = append(refs, prevBlocks[op.Index])
			continue
		}
		for off := 0; off < len(op.Data); off += delta.DefaultBlockSize {
			end := off + delta.DefaultBlockSize
			if end > len(op.Data) {
				end = len(op.Data)
			}
			ref, err := uploadBlockIfNew(ctx, dest, planName, op.Data[off:end], encKey, limiter, total)
			if err != nil {
				return nil, err
			}
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

// blockReader streams the plaintext contents of a block reference list in
// order, downloading (and decrypting) one block at a time. It is the base
// reader for the rolling-checksum signature of a previous version.
type blockReader struct {
	ctx     context.Context
	dest    storage.Storage
	plan    string
	blocks  []blockRef
	encKey  []byte
	limiter *ratelimit.Limiter
	cur     []byte
	off     int
	idx     int
}

func (r *blockReader) Read(p []byte) (int, error) {
	for {
		if r.off < len(r.cur) {
			n := copy(p, r.cur[r.off:])
			r.off += n
			return n, nil
		}
		if r.idx >= len(r.blocks) {
			return 0, io.EOF
		}
		blk, err := downloadBlock(r.ctx, r.dest, r.plan, r.blocks[r.idx], r.encKey, r.limiter)
		if err != nil {
			return 0, err
		}
		r.cur, r.off = blk, 0
		r.idx++
	}
}

// backupDumpBlocks streams a source dump (database) into storage. When the
// previous snapshot has blocks for the same source, the new dump is diffed
// against them with the rsync-style rolling delta: matching ranges become
// references to already-stored blocks and only the differing literals are
// uploaded. Without a base, the dump is split into fixed-size
// content-addressed blocks, uploading only blocks not already in storage.
func (e *Engine) backupDumpBlocks(ctx context.Context, dest storage.Storage, planName string, r io.Reader, prevBlocks []blockRef, encKey []byte, limiter *ratelimit.Limiter) (int64, []blockRef, error) {
	var total int64
	if len(prevBlocks) > 0 {
		refs, err := e.backupStreamWithRollingDelta(ctx, dest, planName, r, prevBlocks, encKey, limiter, &total)
		return total, refs, err
	}

	var refs []blockRef
	buf := make([]byte, delta.DefaultBlockSize)
	for {
		n, readErr := io.ReadFull(r, buf)
		if n == 0 && readErr == io.EOF {
			break
		}
		if n == 0 && readErr != nil {
			return 0, nil, fmt.Errorf("reading dump: %w", readErr)
		}

		ref, err := uploadBlockIfNew(ctx, dest, planName, buf[:n], encKey, limiter, &total)
		if err != nil {
			return 0, nil, err
		}
		refs = append(refs, ref)

		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			return 0, nil, fmt.Errorf("reading dump: %w", readErr)
		}
	}
	return total, refs, nil
}

// downloadBlock fetches a block, decrypting it when the plan is encrypted
// and always verifying the plaintext against the content hash recorded in
// the manifest. Unencrypted backups get the same integrity guarantee as
// encrypted ones: restore never writes silently corrupted data.
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
	}
	if block.Hash != "" {
		plainHash, err := hex.DecodeString(block.Hash)
		if err != nil {
			return nil, fmt.Errorf("invalid block hash: %w", err)
		}
		computed := sha256.Sum256(plain)
		if !bytes.Equal(computed[:], plainHash) {
			return nil, fmt.Errorf("block %s: hash mismatch (corrupt)", block.ID)
		}
	}
	return plain, nil
}

// restoreDumpBlocks reassembles a dump source from its block references,
// writing the plaintext dump to target/<basename>. The dump is written to a
// temp file and renamed into place only after every block is verified, so a
// corrupt block never leaves a truncated dump behind.
func (e *Engine) restoreDumpBlocks(ctx context.Context, dest storage.Storage, planName, srcKey, target string, blocks []blockRef, encKey []byte, limiter *ratelimit.Limiter) error {
	if err := os.MkdirAll(target, 0755); err != nil {
		return fmt.Errorf("creating target dir: %w", err)
	}
	tmp, err := os.CreateTemp(target, ".backupd-restore-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	removeTmp := func() {
		tmp.Close()
		os.Remove(tmpName)
	}
	for _, block := range blocks {
		plain, err := downloadBlock(ctx, dest, planName, block, encKey, limiter)
		if err != nil {
			removeTmp()
			return err
		}
		if _, err := tmp.Write(plain); err != nil {
			removeTmp()
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	out := filepath.Join(target, filepath.Base(srcKey))
	if err := os.Rename(tmpName, out); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
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
	// carry no blocks; they are recreated in pass 4. Each file is written
	// to a temp file in the target tree and renamed into place only after
	// every block downloaded and verified, so a corrupt block never
	// leaves a half-written file behind.
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

		tmp, err := os.CreateTemp(filepath.Dir(outPath), ".backupd-restore-*")
		if err != nil {
			return err
		}
		tmpName := tmp.Name()
		removeTmp := func() {
			tmp.Close()
			os.Remove(tmpName)
		}

		for _, block := range ref.Blocks {
			plain, err := downloadBlock(ctx, dest, planName, block, encKey, limiter)
			if err != nil {
				removeTmp()
				return err
			}
			if _, err := tmp.Write(plain); err != nil {
				removeTmp()
				return err
			}
		}
		if err := tmp.Chmod(ref.Mode.Perm()); err != nil {
			removeTmp()
			return err
		}
		if err := tmp.Close(); err != nil {
			os.Remove(tmpName)
			return err
		}
		if err := os.Rename(tmpName, outPath); err != nil {
			os.Remove(tmpName)
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
