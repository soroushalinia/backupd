package retention

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/xero/backupd/internal/state"
	"github.com/xero/backupd/internal/storage"
)

type Pruner struct {
	store *state.Store
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

	log.Printf("prune %q: deleting %d snapshots", plan, len(toDelete))

	usedBlocks := make(map[string]bool)
	allBlocks := make(map[string]bool)

	for _, snap := range toDelete {
		if err := p.deleteSnapshot(ctx, dest, plan, snap.ID); err != nil {
			log.Printf("error deleting snapshot %s from storage: %v", snap.ID, err)
			continue
		}
		if err := p.store.DeleteSnapshot(plan, snap.ID); err != nil {
			log.Printf("error deleting snapshot %s from state: %v", snap.ID, err)
		}
	}

	keepSummaries, _ := policy.Evaluate(summaries)
	for _, s := range keepSummaries {
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
			blockKey := fmt.Sprintf("%s/blocks/%s", plan, id)
			if err := dest.Delete(ctx, blockKey); err != nil {
				log.Printf("error deleting orphan block %s: %v", id, err)
			} else {
				orphaned++
			}
		}
	}

	if orphaned > 0 {
		log.Printf("prune %q: removed %d orphaned blocks", plan, orphaned)
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
	for _, obj := range objects {
		fullKey := fmt.Sprintf("%s/%s", prefix, obj.Key)
		if err := dest.Delete(ctx, fullKey); err != nil {
			log.Printf("error deleting %s: %v", fullKey, err)
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
			Files []struct {
				BlockIDs []string `json:"block_ids"`
			} `json:"files"`
		} `json:"sources"`
	}

	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, nil
	}

	var blocks []string
	for _, src := range manifest.Sources {
		for _, f := range src.Files {
			blocks = append(blocks, f.BlockIDs...)
		}
	}
	return blocks, nil
}
