package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/soroushalinia/backupd/internal/storage"
)

func (e *Engine) Restore(ctx context.Context, plan string, snapshotID string, target string, dest storage.Storage) error {
	manifestKey := fmt.Sprintf("%s/snapshots/%s/manifest.json", plan, snapshotID)

	r, err := dest.Download(ctx, manifestKey)
	if err != nil {
		return fmt.Errorf("downloading manifest: %w", err)
	}
	if r == nil {
		return fmt.Errorf("manifest for snapshot %q not found", snapshotID)
	}
	defer r.Close()

	manifestData, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	var manifest struct {
		Sources []sourceEntry `json:"sources"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("parsing manifest: %w", err)
	}

	for _, src := range manifest.Sources {
		switch src.Type {
		case "file":
			fm := &fileManifest{Files: src.Files}
			if err := e.restoreFilesWithDelta(ctx, dest, plan, target, fm); err != nil {
				return err
			}

		default:
			if src.Key == "" {
				return fmt.Errorf("missing source key for type %q", src.Type)
			}
			if err := e.restoreSource(ctx, dest, src.Key, target); err != nil {
				return err
			}
		}
	}

	return nil
}

func (e *Engine) restoreSource(ctx context.Context, dest storage.Storage, srcKey, target string) error {
	r, err := dest.Download(ctx, srcKey)
	if err != nil {
		return fmt.Errorf("downloading %q: %w", srcKey, err)
	}
	if r == nil {
		return fmt.Errorf("source %q not found", srcKey)
	}
	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(target, 0755); err != nil {
		return fmt.Errorf("creating target dir: %w", err)
	}
	out := filepath.Join(target, filepath.Base(srcKey))
	return os.WriteFile(out, data, 0644)
}


