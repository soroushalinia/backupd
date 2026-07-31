package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/soroushalinia/backupd/internal/config"
	"github.com/soroushalinia/backupd/internal/crypto"
	"github.com/soroushalinia/backupd/internal/hook"
	"github.com/soroushalinia/backupd/internal/progress"
	"github.com/soroushalinia/backupd/internal/retention"
	"github.com/soroushalinia/backupd/internal/source"
	"github.com/soroushalinia/backupd/internal/state"
	"github.com/soroushalinia/backupd/internal/storage"
	"github.com/soroushalinia/backupd/internal/tag"
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

	hr := hook.NewRunner().
		WithEnv("BACKUPD_PLAN", plan.Name).
		WithEnv("BACKUPD_SNAPSHOT_ID", snapID).
		WithEnv("BACKUPD_TIMESTAMP", time.Now().UTC().Format(time.RFC3339)).
		WithEnv("BACKUPD_STATUS", "running")

	if plan.Hooks != nil {
		if err := hr.RunAll(ctx, plan.Hooks.PreBackup); err != nil {
			return nil, fmt.Errorf("pre-backup hook: %w", err)
		}
	}

	totalSize, err := e.runSources(ctx, dest, plan, snapID)

	if err != nil {
		if plan.Hooks != nil {
			hr.WithEnv("BACKUPD_STATUS", "failure")
			if hookErr := hr.RunAll(ctx, plan.Hooks.OnFailure); hookErr != nil {
				log.Printf("on-failure hook error: %v", hookErr)
			}
		}
		return nil, fmt.Errorf("backup failed: %w", err)
	}

	if plan.Hooks != nil {
		hr.WithEnv("BACKUPD_STATUS", "success")
		if err := hr.RunAll(ctx, plan.Hooks.PostBackup); err != nil {
			log.Printf("post-backup hook error: %v", err)
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

	if plan.Retention != nil {
		pruner := retention.NewPruner(e.store)
		policy := retention.FromConfig(plan.Retention)
		if err := pruner.Prune(ctx, plan.Name, policy, dest); err != nil {
			log.Printf("prune error for %q: %v", plan.Name, err)
		}
	}

	elapsed := time.Since(start)
	log.Printf("backup %q complete: snapshot=%s size=%d duration=%s", plan.Name, snapID, totalSize, elapsed)

	return &RunResult{
		SnapshotID: snapID,
		Size:       totalSize,
		Duration:   elapsed,
	}, nil
}

func (e *Engine) runSources(ctx context.Context, dest storage.Storage, plan config.Plan, snapID string) (int64, error) {
	var fileManifests []*fileManifest
	totalSize := int64(0)

	tags := make(map[string]string)
	for k, v := range plan.Tags {
		tags[k] = v
	}
	for k, v := range tag.ReservedTags(plan.Name, snapID, time.Now().UTC().Format(time.RFC3339), len(plan.Sources)) {
		tags[k] = v
	}

	encInfo, encKey, err := encryptionKey(plan.Encryption)
	if err != nil {
		return 0, fmt.Errorf("encryption setup: %w", err)
	}

	for i, srcCfg := range plan.Sources {
		log.Printf("  source %d: type=%s", i, srcCfg.Type)

		var srcKey string
		var r io.ReadCloser
		var srcErr error

		switch srcCfg.Type {
		case "file":
			log.Printf("  source %d: backing up files from %s ...", i, srcCfg.Path)
			size, fm, err := e.backupFilesWithDelta(ctx, dest, plan.Name, srcCfg.Path, srcCfg.Exclude, encKey)
			if err != nil {
				return 0, fmt.Errorf("backing up files: %w", err)
			}
			totalSize += size
			fileManifests = append(fileManifests, fm)
			continue

		case "database":
			log.Printf("  source %d: dumping database (%s) ...", i, srcCfg.Adapter)
			dbSrc, err := source.NewDatabaseSource(srcCfg.Adapter, srcCfg.DSN, srcCfg.DumpTool)
			if err != nil {
				return 0, fmt.Errorf("database source: %w", err)
			}
			srcKey = fmt.Sprintf("%s/snapshots/%s/sources/%d.sql", plan.Name, snapID, i)
			r, srcErr = dbSrc.Capture(ctx)

		case "docker":
			srcKey = fmt.Sprintf("%s/snapshots/%s/sources/%d.tar", plan.Name, snapID, i)
			r, srcErr = source.NewDockerSource(srcCfg.Volume).Capture(ctx)

		case "kubernetes":
			srcKey = fmt.Sprintf("%s/snapshots/%s/sources/%d.tar", plan.Name, snapID, i)
			r, srcErr = source.NewK8sSource(srcCfg.PVC, srcCfg.Snapshot).Capture(ctx)

		default:
			src, err := sourceFromConfig(srcCfg)
			if err != nil {
				return 0, fmt.Errorf("source %d: %w", i, err)
			}
			srcKey = fmt.Sprintf("%s/snapshots/%s/sources/%d.tar.gz", plan.Name, snapID, i)
			r, srcErr = src.Capture(ctx)
		}

		if srcErr != nil {
			return 0, fmt.Errorf("capturing source %d: %w", i, srcErr)
		}

		size, err := uploadAndEncrypt(ctx, dest, srcKey, r, encKey)
		r.Close()
		if err != nil {
			return 0, fmt.Errorf("uploading source %d: %w", i, err)
		}
		totalSize += size

		if len(tags) > 0 {
			_ = dest.SetTags(ctx, srcKey, tags)
		}
	}

	log.Printf("  total uploaded: %s", formatBytes(totalSize))

	manifestKey := fmt.Sprintf("%s/snapshots/%s/manifest.json", plan.Name, snapID)
	var sourceEntries []sourceEntry
	for i, srcCfg := range plan.Sources {
		if srcCfg.Type == "file" {
			continue
		}
		var ext string
		switch srcCfg.Type {
		case "database":
			ext = ".sql"
		case "docker", "kubernetes":
			ext = ".tar"
		default:
			ext = ".tar.gz"
		}
		srcKey := fmt.Sprintf("%s/snapshots/%s/sources/%d%s", plan.Name, snapID, i, ext)
		sourceEntries = append(sourceEntries, sourceEntry{Type: srcCfg.Type, Key: srcKey})
	}

	merged := &fileManifest{}
	for _, fm := range fileManifests {
		merged.Files = append(merged.Files, fm.Files...)
	}
	if len(merged.Files) > 0 {
		sourceEntries = append(sourceEntries, sourceEntry{Type: "file", Files: merged.Files})
	}

	if err := writeSnapshotManifest(ctx, dest, manifestKey, plan.Name, snapID, totalSize, sourceEntries, plan.Tags, encInfo); err != nil {
		return 0, fmt.Errorf("writing manifest: %w", err)
	}
	if len(tags) > 0 {
		_ = dest.SetTags(ctx, manifestKey, tags)
	}

	return totalSize, nil
}

func uploadAndEncrypt(ctx context.Context, dest storage.Storage, key string, r io.Reader, encKey []byte) (int64, error) {
	pr := progress.NewReader(r, key)
	data, err := io.ReadAll(pr)
	if err != nil {
		return 0, err
	}
	pr.Done()

	if encKey != nil {
		encrypted, err := crypto.Encrypt(encKey, data)
		if err != nil {
			return 0, fmt.Errorf("encrypting: %w", err)
		}
		log.Printf("  uploading %s (encrypted, %s)", key, formatBytes(int64(len(encrypted))))
		if err := dest.Upload(ctx, key+".enc", bytes.NewReader(encrypted)); err != nil {
			return 0, err
		}
		return int64(len(encrypted)), nil
	}

	log.Printf("  uploading %s (%s)", key, formatBytes(int64(len(data))))
	if err := dest.Upload(ctx, key, bytes.NewReader(data)); err != nil {
		return 0, err
	}
	return int64(len(data)), nil
}

type encryptionInfo struct {
	Algorithm string `json:"algorithm,omitempty"`
	KDF       string `json:"kdf,omitempty"`
	Salt      []byte `json:"salt,omitempty"`
}

func encryptionKey(enc *config.Encryption) (*encryptionInfo, []byte, error) {
	if enc == nil || enc.Passphrase == "" {
		return nil, nil, nil
	}
	salt, err := crypto.GenerateSalt()
	if err != nil {
		return nil, nil, err
	}
	return &encryptionInfo{
		Algorithm: "AES-256-GCM",
		KDF:       "Argon2id",
		Salt:      salt,
	}, crypto.DeriveKey(enc.Passphrase, salt), nil
}

// planKey derives the encryption key for a snapshot from the plan's
// passphrase and the salt recorded in the snapshot manifest.
func planKey(plan *config.Plan, salt []byte) ([]byte, error) {
	if len(salt) == 0 {
		return nil, nil
	}
	if plan.Encryption == nil || plan.Encryption.Passphrase == "" {
		return nil, fmt.Errorf("snapshot is encrypted but plan %q has no encryption.passphrase", plan.Name)
	}
	return crypto.DeriveKey(plan.Encryption.Passphrase, salt), nil
}

type sourceEntry struct {
	Type  string         `json:"type"`
	Key   string         `json:"key,omitempty"`
	Files []fileBlockRef `json:"files,omitempty"`
}

func writeSnapshotManifest(ctx context.Context, dest storage.Storage, key, planName, snapID string, totalSize int64, sources []sourceEntry, tags map[string]string, encInfo *encryptionInfo) error {
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
		Sources:    sources,
		Tags:       tags,
		Encryption: encInfo,
	}

	data, err := json.MarshalIndent(sm, "", "  ")
	if err != nil {
		return err
	}
	return dest.Upload(ctx, key, bytes.NewReader(data))
}

func sourceFromConfig(cfg config.Source) (source.Source, error) {
	switch cfg.Type {
	case "file":
		return source.NewFileSource(cfg.Path, cfg.Exclude), nil
	default:
		return nil, fmt.Errorf("unsupported source type: %q", cfg.Type)
	}
}
