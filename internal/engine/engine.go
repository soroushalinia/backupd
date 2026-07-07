package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/xero/backupd/internal/config"
	"github.com/xero/backupd/internal/source"
	"github.com/xero/backupd/internal/state"
	"github.com/xero/backupd/internal/storage"
)

type Engine struct {
	store *state.Store
}

func New(store *state.Store) *Engine {
	return &Engine{store: store}
}

type RunResult struct {
	SnapshotID string
	Size       int64
	Duration   time.Duration
}

func (e *Engine) Run(ctx context.Context, plan config.Plan, dest storage.Storage) (*RunResult, error) {
	log.Printf("starting backup for plan %q", plan.Name)
	start := time.Now()

	snapID := newSnapshotID()
	totalSize := int64(0)

	var fileManifests []*fileManifest

	for i, srcCfg := range plan.Sources {
		switch srcCfg.Type {
		case "file":
			size, fm, err := e.backupFilesWithDelta(ctx, dest, plan.Name, srcCfg.Path, srcCfg.Exclude)
			if err != nil {
				return nil, fmt.Errorf("backing up files: %w", err)
			}
			totalSize += size
			fileManifests = append(fileManifests, fm)

		default:
			src, err := sourceFromConfig(srcCfg)
			if err != nil {
				return nil, fmt.Errorf("source %d: %w", i, err)
			}
			srcKey := fmt.Sprintf("%s/snapshots/%s/sources/%d.tar.gz", plan.Name, snapID, i)
			r, err := src.Capture(ctx)
			if err != nil {
				return nil, fmt.Errorf("capturing source %d: %w", i, err)
			}
			size, err := uploadStream(ctx, dest, srcKey, r)
			r.Close()
			if err != nil {
				return nil, fmt.Errorf("uploading source %d: %w", i, err)
			}
			totalSize += size
		}
	}

	if len(fileManifests) > 0 {
		merged := &fileManifest{}
		for _, fm := range fileManifests {
			merged.Files = append(merged.Files, fm.Files...)
		}
		if err := writeSnapshotManifest(ctx, dest, plan.Name, snapID, totalSize, merged, plan.Tags); err != nil {
			return nil, fmt.Errorf("writing manifest: %w", err)
		}
	} else {
		manifestKey := fmt.Sprintf("%s/snapshots/%s/manifest.json", plan.Name, snapID)
		if err := writeSimpleManifest(ctx, dest, manifestKey, plan.Name, snapID); err != nil {
			return nil, fmt.Errorf("writing manifest: %w", err)
		}
	}

	snap := config.Snapshot{
		ID:        snapID,
		Plan:      plan.Name,
		Timestamp: time.Now().UTC(),
		Size:      totalSize,
		Tags:      plan.Tags,
	}

	if err := e.store.RecordSnapshot(snap); err != nil {
		return nil, fmt.Errorf("recording snapshot: %w", err)
	}

	elapsed := time.Since(start)
	log.Printf("backup %q complete: snapshot=%s size=%d duration=%s", plan.Name, snapID, totalSize, elapsed)

	return &RunResult{
		SnapshotID: snapID,
		Size:       totalSize,
		Duration:   elapsed,
	}, nil
}

func uploadStream(ctx context.Context, dest storage.Storage, key string, r io.Reader) (int64, error) {
	var buf bytes.Buffer
	written, err := io.Copy(&buf, r)
	if err != nil {
		return 0, err
	}
	if err := dest.Upload(ctx, key, &buf); err != nil {
		return 0, err
	}
	return written, nil
}

func writeSimpleManifest(ctx context.Context, dest storage.Storage, key, planName, snapID string) error {
	manifest := fmt.Sprintf(`{
  "snapshot": %q,
  "plan": %q,
  "timestamp": %q
}`, snapID, planName, time.Now().UTC().Format(time.RFC3339))
	return dest.Upload(ctx, key, bytes.NewReader([]byte(manifest)))
}

func sourceFromConfig(cfg config.Source) (source.Source, error) {
	switch cfg.Type {
	case "file":
		return source.NewFileSource(cfg.Path, cfg.Exclude), nil
	default:
		return nil, fmt.Errorf("unsupported source type: %q", cfg.Type)
	}
}
