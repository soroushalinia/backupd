package engine

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/xero/backupd/internal/storage"
)

func (e *Engine) Restore(ctx context.Context, plan string, snapshotID string, target string, dest storage.Storage) error {
	srcKey := fmt.Sprintf("%s/snapshots/%s/sources/0.tar.gz", plan, snapshotID)

	r, err := dest.Download(ctx, srcKey)
	if err != nil {
		return fmt.Errorf("downloading snapshot: %w", err)
	}
	if r == nil {
		return fmt.Errorf("snapshot %q not found at key %q", snapshotID, srcKey)
	}
	defer r.Close()

	if err := untar(r, target); err != nil {
		return fmt.Errorf("extracting: %w", err)
	}

	return nil
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
