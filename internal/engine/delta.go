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
	Path     string      `json:"path"`
	Size     int64       `json:"size"`
	Mode     os.FileMode `json:"mode"`
	Blocks   []blockRef  `json:"blocks"`
	FileHash string      `json:"file_hash"`
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
		if fi.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		if isExcluded(rel, exclude) {
			return nil
		}
		log.Printf("  delta file: %s (%s)", rel, formatBytes(fi.Size()))

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		fileHash := sha256.Sum256(data)
		fileHashStr := hex.EncodeToString(fileHash[:])

		ref := fileBlockRef{
			Path:     rel,
			Size:     fi.Size(),
			Mode:     fi.Mode(),
			FileHash: fileHashStr,
		}

		// Unchanged file: reuse the block references from the previous
		// manifest without hashing, re-encrypting, or checking the storage.
		if prevRef, ok := prevFiles[rel]; ok && prevRef.FileHash == fileHashStr {
			ref.Blocks = prevRef.Blocks
			manifest.Files = append(manifest.Files, ref)
			return nil
		}

		knownBlocks := make(map[string]bool)
		if prevRef, ok := prevFiles[rel]; ok {
			for _, b := range prevRef.Blocks {
				knownBlocks[b.ID] = true
			}
		}

		sig := delta.SignBytes(data, delta.DefaultBlockSize)
		for i, b := range sig.Blocks {
			start := i * delta.DefaultBlockSize
			end := start + delta.DefaultBlockSize
			if end > len(data) {
				end = len(data)
			}
			block := data[start:end]

			plainHash := b.Strong
			blockID := hex.EncodeToString(plainHash[:])
			stored := block

			if encKey != nil {
				enc, err := crypto.EncryptBlock(encKey, block)
				if err != nil {
					return fmt.Errorf("encrypting block of %s: %w", rel, err)
				}
				stored = enc
				idHash := sha256.Sum256(enc)
				blockID = hex.EncodeToString(idHash[:])
			}

			ref.Blocks = append(ref.Blocks, blockRef{ID: blockID, Hash: hex.EncodeToString(plainHash[:])})

			if knownBlocks[blockID] {
				continue
			}
			blockKey := fmt.Sprintf("%s/blocks/%s", planName, blockID)
			exists, err := dest.Exists(ctx, blockKey)
			if err != nil {
				return fmt.Errorf("checking block %s: %w", blockID, err)
			}
			if !exists {
				if err := dest.Upload(ctx, blockKey, bytes.NewReader(stored)); err != nil {
					return fmt.Errorf("uploading block: %w", err)
				}
				total += int64(len(stored))
			}
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

func (e *Engine) restoreFilesWithDelta(ctx context.Context, dest storage.Storage, planName, target string, manifest *fileManifest, encKey []byte) error {
	for _, ref := range manifest.Files {
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

func isExcluded(rel string, exclude []string) bool {
	for _, ex := range exclude {
		if matched, _ := filepath.Match(ex, rel); matched {
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
