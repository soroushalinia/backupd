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

type restoreManifest struct {
	Sources    []sourceEntry `json:"sources"`
	Encryption *struct {
		Salt []byte `json:"salt"`
	} `json:"encryption,omitempty"`
}

func (e *Engine) loadManifest(ctx context.Context, plan config.Plan, snapshotID string, dest storage.Storage) (*restoreManifest, error) {
	manifestKey := fmt.Sprintf("%s/snapshots/%s/manifest.json", plan.Name, snapshotID)

	r, err := dest.Download(ctx, manifestKey)
	if err != nil {
		return nil, fmt.Errorf("downloading manifest: %w", err)
	}
	if r == nil {
		return nil, fmt.Errorf("manifest for snapshot %q not found", snapshotID)
	}
	defer r.Close()

	var manifest restoreManifest
	if err := json.NewDecoder(r).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	return &manifest, nil
}

func (e *Engine) Restore(ctx context.Context, plan config.Plan, snapshotID string, target string, dest storage.Storage) error {
	manifest, err := e.loadManifest(ctx, plan, snapshotID, dest)
	if err != nil {
		return err
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

		case "database":
			// Newer snapshots store dumps as blocks; older ones as
			// a single encrypted archive object.
			if len(src.Blocks) > 0 {
				if err := e.restoreDumpBlocks(ctx, dest, plan.Name, src.Key, target, src.Blocks, encKey, limiter); err != nil {
					return err
				}
				continue
			}
			fallthrough

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

// RestoreSourceReport describes one source of a snapshot in a dry run.
type RestoreSourceReport struct {
	Type   string
	Key    string
	Files  int
	Size   int64
	Blocks int
	// Available is only meaningful for archive sources (docker,
	// kubernetes, legacy database dumps): it reports whether the
	// archived object exists in storage.
	Available bool
}

// RestoreDryRun reports what a restore would write without writing
// anything: the manifest is read and each source summarized, and archive
// sources are checked for presence.
func (e *Engine) RestoreDryRun(ctx context.Context, plan config.Plan, snapshotID string, dest storage.Storage) ([]RestoreSourceReport, error) {
	manifest, err := e.loadManifest(ctx, plan, snapshotID, dest)
	if err != nil {
		return nil, err
	}

	encrypted := manifest.Encryption != nil

	var reports []RestoreSourceReport
	for _, src := range manifest.Sources {
		rep := RestoreSourceReport{Type: src.Type, Key: src.Key}
		switch src.Type {
		case "file":
			for _, f := range src.Files {
				// Directories are manifest entries so empty dirs
				// restore; only real files carry content.
				if f.Mode.IsDir() {
					continue
				}
				rep.Files++
				rep.Size += f.Size
				rep.Blocks += len(f.Blocks)
			}
		case "database":
			rep.Blocks = len(src.Blocks)
		default:
			if src.Key == "" {
				return nil, fmt.Errorf("missing source key for type %q", src.Type)
			}
			key := src.Key
			if encrypted {
				key += ".enc"
			}
			exists, err := dest.Exists(ctx, key)
			if err != nil {
				return nil, fmt.Errorf("checking %q: %w", key, err)
			}
			rep.Available = exists
		}
		reports = append(reports, rep)
	}
	return reports, nil
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
