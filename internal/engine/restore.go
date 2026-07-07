package engine

import (
	"archive/tar"
	"compress/gzip"
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

	var generic struct {
		Sources []struct {
			Type  string         `json:"type"`
			Files []fileBlockRef `json:"files"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(manifestData, &generic); err != nil {
		return fmt.Errorf("parsing manifest: %w", err)
	}

	for _, src := range generic.Sources {
		switch src.Type {
		case "file":
			fm := &fileManifest{Files: src.Files}
			if err := e.restoreFilesWithDelta(ctx, dest, plan, target, fm); err != nil {
				return err
			}

		default:
			srcKey := fmt.Sprintf("%s/snapshots/%s/sources/0.tar.gz", plan, snapshotID)
			if err := e.restoreTarSource(ctx, dest, srcKey, target); err != nil {
				return err
			}
		}
	}

	return nil
}

func (e *Engine) restoreTarSource(ctx context.Context, dest storage.Storage, srcKey, target string) error {
	r, err := dest.Download(ctx, srcKey)
	if err != nil {
		return fmt.Errorf("downloading source: %w", err)
	}
	if r == nil {
		return fmt.Errorf("source %q not found", srcKey)
	}
	defer r.Close()

	return untar(r, target)
}

func untar(r io.Reader, target string) error {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		path := filepath.Join(target, header.Name)
		info := header.FileInfo()

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, info.Mode()); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return err
			}
			f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, info.Mode())
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}

	return nil
}
