package source

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestFileSourceCapture(t *testing.T) {
	dir := t.TempDir()

	files := map[string]string{
		"a.txt":        "hello world",
		"sub/b.txt":    "nested file",
		"sub/deep/c.txt": "deeply nested",
	}

	for path, content := range files {
		fullPath := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	src := NewFileSource(dir, nil)
	r, err := src.Capture(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	gzr, err := gzip.NewReader(r)
	if err != nil {
		t.Fatal(err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	count := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}

		count++
		switch {
		case hdr.Name == "a.txt":
			data, _ := io.ReadAll(tr)
			if string(data) != "hello world" {
				t.Errorf("a.txt content = %q, want %q", string(data), "hello world")
			}
		case hdr.Name == "sub":
		case hdr.Name == "sub/b.txt":
			data, _ := io.ReadAll(tr)
			if string(data) != "nested file" {
				t.Errorf("b.txt content = %q, want %q", string(data), "nested file")
			}
		case hdr.Name == "sub/deep":
		case hdr.Name == "sub/deep/c.txt":
			data, _ := io.ReadAll(tr)
			if string(data) != "deeply nested" {
				t.Errorf("c.txt content = %q, want %q", string(data), "deeply nested")
			}
		default:
			t.Errorf("unexpected entry: %s", hdr.Name)
		}
	}

	if count != 5 { // 2 dirs + 3 files (root dir is skipped)
		t.Errorf("expected 5 entries, got %d", count)
	}
}

func TestFileSourceExclude(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skip.log"), []byte("skip"), 0644); err != nil {
		t.Fatal(err)
	}

	src := NewFileSource(dir, []string{"*.log"})
	r, err := src.Capture(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	gzr, err := gzip.NewReader(r)
	if err != nil {
		t.Fatal(err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	names := []string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			names = append(names, hdr.Name)
		}
	}

	if len(names) != 1 || names[0] != "keep.txt" {
		t.Errorf("expected only [keep.txt], got %v", names)
	}
}
