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
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xero/backupd/internal/delta"
	"github.com/xero/backupd/internal/storage"
)

type fileBlockRef struct {
	Path     string      `json:"path"`
	Size     int64       `json:"size"`
	Mode     os.FileMode `json:"mode"`
	BlockIDs []string    `json:"block_ids"`
	FileHash string      `json:"file_hash"`
}

type fileManifest struct {
	Files []fileBlockRef `json:"files"`
}

func (e *Engine) backupFilesWithDelta(ctx context.Context, dest storage.Storage, planName, sourceRoot string, exclude []string) (int64, *fileManifest, error) {
	var total int64
	manifest := &fileManifest{}

	err := filepath.Walk(sourceRoot, func(path string, fi os.FileInfo, err error) error {
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

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		fileHash := sha256.Sum256(data)
		fileHashStr := hex.EncodeToString(fileHash[:])

		sigKey := fmt.Sprintf("%s/signatures/%x", planName, sha256.Sum256([]byte(rel)))

		ref := fileBlockRef{
			Path:     rel,
			Size:     fi.Size(),
			Mode:     fi.Mode(),
			FileHash: fileHashStr,
		}

		var prevSig *delta.Signature
		r, err := dest.Download(ctx, sigKey)
		if err == nil && r != nil {
			sigData, _ := io.ReadAll(r)
			r.Close()
			if len(sigData) > 0 {
				prevSig, _ = delta.UnmarshalSignature(sigData)
			}
		}

		if prevSig != nil {
			ops, err := delta.DiffBytes(prevSig, data)
			if err != nil {
				return fmt.Errorf("diffing %s: %w", rel, err)
			}

			for _, op := range ops {
				if op.Copy {
					if op.Index < len(prevSig.Blocks) {
						strong := prevSig.Blocks[op.Index].Strong
						ref.BlockIDs = append(ref.BlockIDs, hex.EncodeToString(strong[:]))
					}
				} else {
					blockID := sha256.Sum256(op.Data)
					blockKey := fmt.Sprintf("%s/blocks/%x", planName, blockID)
					exists, _ := dest.Exists(ctx, blockKey)
					if !exists {
						if err := dest.Upload(ctx, blockKey, bytes.NewReader(op.Data)); err != nil {
							return fmt.Errorf("uploading block: %w", err)
						}
						total += int64(len(op.Data))
					}
					ref.BlockIDs = append(ref.BlockIDs, hex.EncodeToString(blockID[:]))
				}
			}
		} else {
			sig := delta.SignBytes(data, delta.DefaultBlockSize)
			for i, b := range sig.Blocks {
				blockID := hex.EncodeToString(b.Strong[:])
				blockKey := fmt.Sprintf("%s/blocks/%s", planName, blockID)
				exists, _ := dest.Exists(ctx, blockKey)
				if !exists {
					start := i * delta.DefaultBlockSize
					end := start + delta.DefaultBlockSize
					if end > len(data) {
						end = len(data)
					}
					if err := dest.Upload(ctx, blockKey, bytes.NewReader(data[start:end])); err != nil {
						return fmt.Errorf("uploading block: %w", err)
					}
					total += int64(end - start)
				}
				ref.BlockIDs = append(ref.BlockIDs, blockID)
			}
		}

		manifest.Files = append(manifest.Files, ref)

		newSig := delta.SignBytes(data, delta.DefaultBlockSize)
		if err := dest.Upload(ctx, sigKey, bytes.NewReader(delta.MarshalSignature(newSig))); err != nil {
			return fmt.Errorf("uploading signature: %w", err)
		}

		return nil
	})

	if err != nil {
		return 0, nil, err
	}

	return total, manifest, nil
}

func (e *Engine) restoreFilesWithDelta(ctx context.Context, dest storage.Storage, planName, target string, manifest *fileManifest) error {
	for _, ref := range manifest.Files {
		var fileData bytes.Buffer
		for _, blockID := range ref.BlockIDs {
			blockKey := fmt.Sprintf("%s/blocks/%s", planName, blockID)
			r, err := dest.Download(ctx, blockKey)
			if err != nil {
				return fmt.Errorf("downloading block %s: %w", blockID, err)
			}
			if r == nil {
				return fmt.Errorf("block %s not found", blockID)
			}
			_, err = io.Copy(&fileData, r)
			r.Close()
			if err != nil {
				return err
			}
		}

		outPath := filepath.Join(target, ref.Path)
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(outPath, fileData.Bytes(), ref.Mode); err != nil {
			return err
		}
	}
	return nil
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

func writeSnapshotManifest(ctx context.Context, dest storage.Storage, planName, snapID string, totalSize int64, m *fileManifest, tags map[string]string, encInfo *encryptionInfo) error {
	type sourceEntry struct {
		Type  string         `json:"type"`
		Files []fileBlockRef `json:"files,omitempty"`
	}

	type snapManifest struct {
		Snapshot   string            `json:"snapshot"`
		Plan       string            `json:"plan"`
		Timestamp  string            `json:"timestamp"`
		Size       int64             `json:"size"`
		Sources    []sourceEntry     `json:"sources"`
		Encryption *encryptionInfo   `json:"encryption,omitempty"`
		Tags       map[string]string `json:"tags,omitempty"`
	}

	sm := snapManifest{
		Snapshot:   snapID,
		Plan:       planName,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Size:       totalSize,
		Tags:       tags,
		Encryption: encInfo,
		Sources: []sourceEntry{
			{
				Type:  "file",
				Files: m.Files,
			},
		},
	}

	data, err := json.MarshalIndent(sm, "", "  ")
	if err != nil {
		return err
	}

	key := fmt.Sprintf("%s/snapshots/%s/manifest.json", planName, snapID)
	return dest.Upload(ctx, key, bytes.NewReader(data))
}

func newSnapshotID() string {
	return uuid.New().String()
}
