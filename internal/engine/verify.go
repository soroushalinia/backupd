package engine

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/xero/backupd/internal/crypto"
	"github.com/xero/backupd/internal/storage"
)

func (e *Engine) Verify(ctx context.Context, planName string, snapshotID string, dest storage.Storage) error {
	if snapshotID == "" {
		return e.verifyAll(ctx, planName, dest)
	}
	return e.verifyOne(ctx, planName, snapshotID, dest)
}

func (e *Engine) verifyAll(ctx context.Context, planName string, dest storage.Storage) error {
	snapshots, err := e.store.ListSnapshots(planName)
	if err != nil {
		return fmt.Errorf("listing snapshots: %w", err)
	}
	if len(snapshots) == 0 {
		return fmt.Errorf("no snapshots found for plan %q", planName)
	}

	for _, snap := range snapshots {
		if err := e.verifyOne(ctx, planName, snap.ID, dest); err != nil {
			return fmt.Errorf("snapshot %s: %w", snap.ID, err)
		}
	}
	return nil
}

func (e *Engine) verifyOne(ctx context.Context, planName, snapshotID string, dest storage.Storage) error {
	manifestKey := fmt.Sprintf("%s/snapshots/%s/manifest.json", planName, snapshotID)
	r, err := dest.Download(ctx, manifestKey)
	if err != nil {
		return fmt.Errorf("downloading manifest: %w", err)
	}
	if r == nil {
		return fmt.Errorf("manifest not found")
	}
	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("reading manifest: %w", err)
	}

	var manifest struct {
		Snapshot   string          `json:"snapshot"`
		Plan       string          `json:"plan"`
		Sources    json.RawMessage `json:"sources"`
		Encryption *struct {
			Algorithm string `json:"algorithm"`
			KDF       string `json:"kdf"`
			Salt      []byte `json:"salt"`
		} `json:"encryption,omitempty"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parsing manifest: %w", err)
	}

	sources := struct {
		Sources []struct {
			Type  string `json:"type"`
			Files []struct {
				Path     string   `json:"path"`
				BlockIDs []string `json:"block_ids"`
				FileHash string   `json:"file_hash"`
			} `json:"files"`
		} `json:"sources"`
	}{}
	if err := json.Unmarshal(data, &sources); err == nil {
		for _, src := range sources.Sources {
			if src.Type == "file" {
				for _, f := range src.Files {
					if err := verifyFileBlocks(ctx, dest, planName, f.Path, f.BlockIDs, f.FileHash); err != nil {
						return err
					}
				}
			}
		}
	}

	prefix := fmt.Sprintf("%s/snapshots/%s/sources/", planName, snapshotID)
	objects, err := dest.List(ctx, prefix)
	if err != nil {
		return nil
	}

	for _, obj := range objects {
		fullKey := obj.Key
		sr, err := dest.Download(ctx, fullKey)
		if err != nil {
			return fmt.Errorf("downloading source %s: %w", fullKey, err)
		}
		if sr == nil {
			return fmt.Errorf("source %s not found", fullKey)
		}

		archiveData, err := io.ReadAll(sr)
		sr.Close()
		if err != nil {
			return fmt.Errorf("reading source %s: %w", fullKey, err)
		}

		if manifest.Encryption != nil {
			if len(archiveData) < crypto.NonceSize+1 {
				return fmt.Errorf("source %s: invalid encryption envelope", fullKey)
			}
		} else {
			if _, err := gzip.NewReader(bytes.NewReader(archiveData)); err != nil {
				return fmt.Errorf("source %s: invalid gzip: %w", fullKey, err)
			}
		}
	}

	return nil
}

func verifyFileBlocks(ctx context.Context, dest storage.Storage, planName, path string, blockIDs []string, fileHash string) error {
	var fileData bytes.Buffer
	for _, blockID := range blockIDs {
		blockKey := fmt.Sprintf("%s/blocks/%s", planName, blockID)
		br, err := dest.Download(ctx, blockKey)
		if err != nil {
			return fmt.Errorf("downloading block %s for %s: %w", blockID, path, err)
		}
		if br == nil {
			return fmt.Errorf("block %s not found for %s", blockID, path)
		}
		blockData, err := io.ReadAll(br)
		br.Close()
		if err != nil {
			return fmt.Errorf("reading block %s: %w", blockID, err)
		}

		computed := sha256.Sum256(blockData)
		if hex.EncodeToString(computed[:]) != blockID {
			return fmt.Errorf("block %s for %s: hash mismatch (corrupt)", blockID, path)
		}
		fileData.Write(blockData)
	}

	if fileHash != "" {
		computed := sha256.Sum256(fileData.Bytes())
		if hex.EncodeToString(computed[:]) != fileHash {
			return fmt.Errorf("file %s: content hash mismatch", path)
		}
	}
	return nil
}
