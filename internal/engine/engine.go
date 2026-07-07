package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/google/uuid"
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

	snapID := uuid.New().String()
	manifestKey := fmt.Sprintf("%s/snapshots/%s/manifest.json", plan.Name, snapID)

	totalSize := int64(0)

	for i, srcCfg := range plan.Sources {
		src, err := sourceFromConfig(srcCfg)
		if err != nil {
			return nil, fmt.Errorf("source %d: %w", i, err)
		}

		srcKey := fmt.Sprintf("%s/snapshots/%s/sources/%d.tar.gz", plan.Name, snapID, i)

		r, err := src.Capture(ctx)
		if err != nil {
			return nil, fmt.Errorf("capturing source %d: %w", i, err)
		}

		size, err := e.uploadStream(ctx, dest, srcKey, r)
		r.Close()
		if err != nil {
			return nil, fmt.Errorf("uploading source %d: %w", i, err)
		}
		totalSize += size
	}

	if err := e.writeManifest(ctx, dest, manifestKey, plan, snapID); err != nil {
		return nil, fmt.Errorf("writing manifest: %w", err)
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

func (e *Engine) uploadStream(ctx context.Context, dest storage.Storage, key string, r io.Reader) (int64, error) {
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

func (e *Engine) writeManifest(ctx context.Context, dest storage.Storage, key string, plan config.Plan, snapID string) error {
	manifest := fmt.Sprintf(`{
  "snapshot": %q,
  "plan": %q,
  "timestamp": %q,
  "sources": %d
}`, snapID, plan.Name, time.Now().UTC().Format(time.RFC3339), len(plan.Sources))

	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte(manifest))
		pw.Close()
	}()

	return dest.Upload(ctx, key, pr)
}

func sourceFromConfig(cfg config.Source) (source.Source, error) {
	switch cfg.Type {
	case "file":
		return source.NewFileSource(cfg.Path, cfg.Exclude), nil
	default:
		return nil, fmt.Errorf("unsupported source type: %q", cfg.Type)
	}
}
