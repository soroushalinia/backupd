package hook

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunSimpleCommand(t *testing.T) {
	r := NewRunner()
	err := r.Run(context.Background(), "echo hello")
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunShellCommand(t *testing.T) {
	r := NewRunner()
	err := r.Run(context.Background(), "echo hello world | awk '{print $1}'")
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunAll(t *testing.T) {
	r := NewRunner()
	err := r.RunAll(context.Background(), []string{
		"echo first",
		"echo second",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunWithEnv(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	r := NewRunner().WithEnv("BACKUPD_TEST", "testval")
	err := r.Run(context.Background(), "echo $BACKUPD_TEST > "+out)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "testval\n" {
		t.Errorf("expected 'testval\\n', got %q", string(data))
	}
}

func TestRunFailure(t *testing.T) {
	r := NewRunner()
	err := r.Run(context.Background(), "false")
	if err == nil {
		t.Fatal("expected error for failing command")
	}
}

func TestRunEmpty(t *testing.T) {
	r := NewRunner()
	err := r.Run(context.Background(), "")
	if err != nil {
		t.Fatal("expected no error for empty command")
	}
}
