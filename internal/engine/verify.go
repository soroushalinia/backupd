package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/soroushalinia/backupd/internal/config"
	"github.com/soroushalinia/backupd/internal/crypto"
	"github.com/soroushalinia/backupd/internal/storage"
)

func (e *Engine) Verify(ctx context.Context, plan config.Plan, snapshotID string, dest storage.Storage) error {
	if snapshotID == "" {
		return e.verifyAll(ctx, plan, dest)
	}
	return e.verifyOne(ctx, plan, snapshotID, dest)
}

func (e *Engine) verifyAll(ctx context.Context, plan config.Plan, dest storage.Storage) error {
	snapshots, err := e.store.ListSnapshots(plan.Name)
	if err != nil {
		return fmt.Errorf("listing snapshots: %w", err)
	}
	if len(snapshots) == 0 {
		return fmt.Errorf("no snapshots found for plan %q", plan.Name)
	}

	for _, snap := range snapshots {
		if err := e.verifyOne(ctx, plan, snap.ID, dest); err != nil {
			return fmt.Errorf("snapshot %s: %w", snap.ID, err)
		}
	}
	return nil
}

func (e *Engine) verifyOne(ctx context.Context, plan config.Plan, snapshotID string, dest storage.Storage) error {
	manifestKey := fmt.Sprintf("%s/snapshots/%s/manifest.json", plan.Name, snapshotID)
	r, err := dest.Download(ctx, manifestKey)
	if err != nil {
		return fmt.Errorf("downloading manifest: %w", err)
	}
	if r == nil {
		return fmt.Errorf("manifest not found")
	}
	defer r.Close()

	var manifest struct {
		Sources    []sourceEntry `json:"sources"`
		Encryption *struct {
			Salt []byte `json:"salt"`
		} `json:"encryption,omitempty"`
	}
	if err := json.NewDecoder(r).Decode(&manifest); err != nil {
		return fmt.Errorf("parsing manifest: %w", err)
	}

	var encKey []byte
	if manifest.Encryption != nil {
		encKey, err = planKey(&plan, manifest.Encryption.Salt)
		if err != nil {
			return err
		}
	}

	for _, src := range manifest.Sources {
		if src.Type == "file" {
			for _, f := range src.Files {
				if err := verifyFileBlocks(ctx, dest, plan.Name, f.Path, f.Blocks, encKey); err != nil {
					return err
				}
			}
			continue
		}

		if src.Key == "" {
			return fmt.Errorf("missing source key for type %q", src.Type)
		}
		if err := verifySource(ctx, dest, src.Key, encKey); err != nil {
			return err
		}
	}

	return nil
}

func verifySource(ctx context.Context, dest storage.Storage, srcKey string, encKey []byte) error {
	key := srcKey
	if encKey != nil {
		key += ".enc"
	}

	sr, err := dest.Download(ctx, key)
	if err != nil {
		return fmt.Errorf("downloading source %s: %w", key, err)
	}
	if sr == nil {
		return fmt.Errorf("source %s not found", key)
	}
	defer sr.Close()

	data, err := io.ReadAll(sr)
	if err != nil {
		return fmt.Errorf("reading source %s: %w", key, err)
	}

	if encKey != nil {
		if _, err := crypto.Decrypt(encKey, data); err != nil {
			return fmt.Errorf("source %s: decryption failed: %w", srcKey, err)
		}
	}

	return nil
}

func verifyFileBlocks(ctx context.Context, dest storage.Storage, planName, path string, blocks []blockRef, encKey []byte) error {
	hash := sha256.New()
	for _, block := range blocks {
		blockKey := fmt.Sprintf("%s/blocks/%s", planName, block.ID)
		br, err := dest.Download(ctx, blockKey)
		if err != nil {
			return fmt.Errorf("downloading block %s for %s: %w", block.ID, path, err)
		}
		if br == nil {
			return fmt.Errorf("block %s not found for %s", block.ID, path)
		}
		blockData, err := io.ReadAll(br)
		br.Close()
		if err != nil {
			return fmt.Errorf("reading block %s: %w", block.ID, err)
		}

		plain := blockData
		if encKey != nil {
			plainHash, err := hex.DecodeString(block.Hash)
			if err != nil {
				return fmt.Errorf("invalid block hash for %s: %w", path, err)
			}
			plain, err = crypto.DecryptBlock(encKey, plainHash, blockData)
			if err != nil {
				return fmt.Errorf("block %s for %s: decryption failed (corrupt): %w", block.ID, path, err)
			}
			if computed := sha256.Sum256(plain); !bytes.Equal(computed[:], plainHash) {
				return fmt.Errorf("block %s for %s: hash mismatch (corrupt)", block.ID, path)
			}
		}
		hash.Write(plain)
	}
	return nil
}
