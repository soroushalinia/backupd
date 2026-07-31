package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/soroushalinia/backupd/internal/config"
	"github.com/soroushalinia/backupd/internal/crypto"
	"github.com/soroushalinia/backupd/internal/ratelimit"
	"github.com/soroushalinia/backupd/internal/storage"
)

func (e *Engine) Restore(ctx context.Context, plan config.Plan, snapshotID string, target string, dest storage.Storage) error {
	manifestKey := fmt.Sprintf("%s/snapshots/%s/manifest.json", plan.Name, snapshotID)

	r, err := dest.Download(ctx, manifestKey)
	if err != nil {
		return fmt.Errorf("downloading manifest: %w", err)
	}
	if r == nil {
		return fmt.Errorf("manifest for snapshot %q not found", snapshotID)
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

	limiter, err := rateLimiter(plan)
	if err != nil {
		return err
	}

	for _, src := range manifest.Sources {
		switch src.Type {
		case "file":
			fm := &fileManifest{Files: src.Files}
			if err := e.restoreFilesWithDelta(ctx, dest, plan.Name, target, fm, encKey, limiter); err != nil {
				return err
			}

		default:
			if src.Key == "" {
				return fmt.Errorf("missing source key for type %q", src.Type)
			}
			if err := e.restoreSource(ctx, dest, src.Key, target, encKey, limiter); err != nil {
				return err
			}
		}
	}

	return nil
}

func (e *Engine) restoreSource(ctx context.Context, dest storage.Storage, srcKey, target string, encKey []byte, limiter *ratelimit.Limiter) error {
	key := srcKey
	if encKey != nil {
		key += ".enc"
	}

	r, err := dest.Download(ctx, key)
	if err != nil {
		return fmt.Errorf("downloading %q: %w", key, err)
	}
	if r == nil {
		return fmt.Errorf("source %q not found", key)
	}
	defer r.Close()
	r = ratelimit.WrapReadCloser(ctx, r, limiter)

	if err := os.MkdirAll(target, 0755); err != nil {
		return fmt.Errorf("creating target dir: %w", err)
	}

	out := filepath.Join(target, filepath.Base(srcKey))
	f, err := os.Create(out)
	if err != nil {
		return err
	}

	if encKey != nil {
		if err := crypto.StreamDecrypt(encKey, r, f); err != nil {
			f.Close()
			return fmt.Errorf("decrypting %q: %w", srcKey, err)
		}
	} else if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}

	return f.Close()
}
