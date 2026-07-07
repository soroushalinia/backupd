package source

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type FileSource struct {
	path    string
	exclude []string
}

func NewFileSource(path string, exclude []string) *FileSource {
	return &FileSource{path: path, exclude: exclude}
}

func (s *FileSource) Type() string { return "file" }

func (s *FileSource) Name() string { return s.path }

func (s *FileSource) Capture(ctx context.Context) (io.ReadCloser, error) {
	pr, pw := io.Pipe()

	go func() {
		err := s.tar(ctx, pw)
		pw.CloseWithError(err)
	}()

	return pr, nil
}

func (s *FileSource) tar(ctx context.Context, w io.WriteCloser) error {
	defer w.Close()

	gw := gzip.NewWriter(w)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	base := s.path

	return filepath.Walk(s.path, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		if s.isExcluded(rel) {
			if fi.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		header, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return fmt.Errorf("header for %s: %w", path, err)
		}
		header.Name = rel

		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("writing header for %s: %w", path, err)
		}

		if !fi.IsDir() && fi.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			if _, err := io.Copy(tw, f); err != nil {
				f.Close()
				return fmt.Errorf("writing %s: %w", path, err)
			}
			f.Close()
		}

		return nil
	})
}

func (s *FileSource) isExcluded(rel string) bool {
	for _, ex := range s.exclude {
		if matched, _ := filepath.Match(ex, rel); matched {
			return true
		}
		if strings.Contains(rel, ex) {
			return true
		}
	}
	return false
}
