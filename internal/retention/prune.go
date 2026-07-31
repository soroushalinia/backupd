package retention

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/soroushalinia/backupd/internal/state"
	"github.com/soroushalinia/backupd/internal/storage"
)

type Pruner struct {
	store *state.Store
	// DryRun reports what would be deleted without deleting anything.
	DryRun bool
}

func NewPruner(store *state.Store) *Pruner {
	return &Pruner{store: store}
}

func (p *Pruner) Prune(ctx context.Context, plan string, policy Policy, dest storage.Storage) error {
	snapshots, err := p.store.ListSnapshots(plan)
	if err != nil {
		return fmt.Errorf("listing snapshots: %w", err)
	}

	var summaries []SnapshotSummary
	for _, s := range snapshots {
		summaries = append(summaries, SnapshotSummary{
			ID:        s.ID,
			Timestamp: s.Timestamp,
			Size:      s.Size,
		})
	}

	_, toDelete := policy.Evaluate(summaries)
	if len(toDelete) == 0 {
		log.Printf("prune %q: nothing to delete", plan)
		return nil
	}

	verb := "deleting"
	if p.DryRun {
		verb = "would delete"
	}
	log.Printf("prune %q: %s %d snapshots", plan, verb, len(toDelete))

	for _, snap := range toDelete {
		if p.DryRun {
			log.Printf("prune %q: would delete snapshot %s (from %s)", plan, snap.ID, snap.Timestamp.Format("2006-01-02 15:04"))
			continue
		}
		if err := p.deleteSnapshot(ctx, dest, plan, snap.ID); err != nil {
			log.Printf("error deleting snapshot %s from storage: %v", snap.ID, err)
			continue
		}
		if err := p.store.DeleteSnapshot(plan, snap.ID); err != nil {
			log.Printf("error deleting snapshot %s from state: %v", snap.ID, err)
		}
	}

	return p.GCBlocks(ctx, plan, dest)
}

// GCBlocks deletes content-addressed block objects that are not referenced
// by any snapshot manifest still recorded in state. Blocks are only ever
// removed here; everything else in the plan's namespace is left alone.
func (p *Pruner) GCBlocks(ctx context.Context, plan string, dest storage.Storage) error {
	usedBlocks := make(map[string]bool)
	allBlocks := make(map[string]bool)

	snapshots, err := p.store.ListSnapshots(plan)
	if err != nil {
		return fmt.Errorf("listing snapshots: %w", err)
	}
	for _, s := range snapshots {
		blocks, err := p.collectBlocks(ctx, dest, plan, s.ID)
		if err != nil {
			log.Printf("error collecting blocks for %s: %v", s.ID, err)
			continue
		}
		for _, b := range blocks {
			usedBlocks[b] = true
		}
	}

	objects, err := dest.List(ctx, plan+"/blocks/")
	if err != nil {
		return fmt.Errorf("listing blocks: %w", err)
	}

	for _, obj := range objects {
		blockID := strings.TrimPrefix(obj.Key, plan+"/blocks/")
		allBlocks[blockID] = true
	}

	var orphaned int
	for id := range allBlocks {
		if !usedBlocks[id] {
			if p.DryRun {
				orphaned++
				continue
			}
			blockKey := fmt.Sprintf("%s/blocks/%s", plan, id)
			if err := dest.Delete(ctx, blockKey); err != nil {
				log.Printf("error deleting orphan block %s: %v", id, err)
			} else {
				orphaned++
			}
		}
	}

	if orphaned > 0 {
		if p.DryRun {
			log.Printf("gc %q: would remove %d orphaned blocks", plan, orphaned)
		} else {
			log.Printf("gc %q: removed %d orphaned blocks", plan, orphaned)
		}
	}

	return nil
}

func (p *Pruner) deleteSnapshot(ctx context.Context, dest storage.Storage, plan, snapID string) error {
	manifestKey := fmt.Sprintf("%s/snapshots/%s/manifest.json", plan, snapID)
	if err := dest.Delete(ctx, manifestKey); err != nil {
		return err
	}

	prefix := fmt.Sprintf("%s/snapshots/%s/sources/", plan, snapID)
	objects, err := dest.List(ctx, prefix)
	if err != nil {
		return err
	}
	// List returns full logical keys (e.g. "plan/snapshots/<id>/sources/0.sql"),
	// so the prefix is not prepended again.
	for _, obj := range objects {
		if err := dest.Delete(ctx, obj.Key); err != nil {
			log.Printf("error deleting %s: %v", obj.Key, err)
		}
	}

	return nil
}

func (p *Pruner) collectBlocks(ctx context.Context, dest storage.Storage, plan, snapID string) ([]string, error) {
	manifestKey := fmt.Sprintf("%s/snapshots/%s/manifest.json", plan, snapID)
	r, err := dest.Download(ctx, manifestKey)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, nil
	}
	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	var manifest struct {
		Sources []struct {
			// Database dump blocks (source-level).
			Blocks []struct {
				ID string `json:"id"`
			} `json:"blocks"`
			Files []struct {
				Blocks []struct {
					ID string `json:"id"`
				} `json:"blocks"`
			} `json:"files"`
		} `json:"sources"`
	}

	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest %s: %w", snapID, err)
	}

	var blocks []string
	for _, src := range manifest.Sources {
		for _, b := range src.Blocks {
			blocks = append(blocks, b.ID)
		}
		for _, f := range src.Files {
			for _, b := range f.Blocks {
				blocks = append(blocks, b.ID)
			}
		}
	}
	return blocks, nil
}
