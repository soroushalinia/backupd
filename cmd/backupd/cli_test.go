package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soroushalinia/backupd/internal/config"
	"github.com/soroushalinia/backupd/internal/state"
)

// runCLI executes the full command tree with args, discarding output.
func runCLI(t *testing.T, args ...string) error {
	t.Helper()
	cmd := newRootCmd()
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd.Execute()
}

// runCLIOut executes the command tree and captures what the commands write
// to stdout (the commands print with fmt.Printf directly).
func runCLIOut(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCmd()
	cmd.SetArgs(args)
	cmd.SetErr(io.Discard)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	execErr := cmd.Execute()
	os.Stdout = old
	w.Close()
	out, _ := io.ReadAll(r)
	r.Close()
	return string(out), execErr
}

// testCLIEnv writes a config file into a temp dir and redirects the state
// database (defaultStatePath) next to it via BACKUPD_CONFIG, so no test
// touches ~/.backupd.yaml.db. It returns the config path.
func testCLIEnv(t *testing.T, cfgYAML string) string {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backupd.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BACKUPD_CONFIG", cfgPath)
	t.Setenv("HOME", dir)
	return cfgPath
}

// validPlan returns one plan entry (no "plans:" header) referencing a
// reachable file source and a syntactically valid s3 destination.
func validPlan(name, srcPath string) string {
	return "  - name: " + name + "\n" +
		"    sources:\n" +
		"      - type: file\n" +
		"        path: " + srcPath + "\n" +
		"    destination:\n" +
		"      type: s3\n" +
		"      bucket: b\n" +
		"      endpoint: example.com\n"
}

func TestCLIList(t *testing.T) {
	cfgPath := testCLIEnv(t, "plans:\n"+validPlan("alpha", t.TempDir())+validPlan("beta", t.TempDir()))

	out, err := runCLIOut(t, "list", "-c", cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Errorf("list should show both plans, got: %s", out)
	}
}

// An empty plans list is rejected by config validation, so the commands
// never see a zero-plan config through the CLI.
func TestCLIListNoPlans(t *testing.T) {
	cfgPath := testCLIEnv(t, "plans: []\n")
	err := runCLI(t, "list", "-c", cfgPath)
	if err == nil || !strings.Contains(err.Error(), "at least one plan is required") {
		t.Fatalf("expected config validation error, got: %v", err)
	}
}

func TestCLICheckOK(t *testing.T) {
	cfgPath := testCLIEnv(t, "plans:\n"+validPlan("p1", t.TempDir()))
	out, err := runCLIOut(t, "check", "-c", cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "configuration OK") {
		t.Errorf("expected configuration OK, got: %s", out)
	}
}

func TestCLICheckInvalid(t *testing.T) {
	cfgPath := testCLIEnv(t, "plans: []\n")
	if err := runCLI(t, "check", "-c", cfgPath); err == nil {
		t.Fatal("expected check to fail on invalid config")
	}
}

func TestCLIRunUnknownPlan(t *testing.T) {
	cfgPath := testCLIEnv(t, "plans:\n"+validPlan("alpha", t.TempDir()))
	err := runCLI(t, "run", "-c", cfgPath, "nope")
	if err == nil {
		t.Fatal("expected error for unknown plan")
	}
	if !strings.Contains(err.Error(), "nope") || !strings.Contains(err.Error(), "alpha") {
		t.Errorf("error should name the plan and list available ones, got: %v", err)
	}
}

// The config validator rejects a file without plans, so an empty config
// surfaces as a clear load-time error rather than a command error.
func TestCLIRunNoPlans(t *testing.T) {
	cfgPath := testCLIEnv(t, "plans: []\n")
	err := runCLI(t, "run", "-c", cfgPath)
	if err == nil || !strings.Contains(err.Error(), "at least one plan is required") {
		t.Fatalf("expected config validation error, got: %v", err)
	}
}

func TestCLIRunUnknownPlanNoPlansConfigured(t *testing.T) {
	cfgPath := testCLIEnv(t, "plans: []\n")
	err := runCLI(t, "run", "-c", cfgPath, "x")
	if err == nil || !strings.Contains(err.Error(), "at least one plan is required") {
		t.Fatalf("expected config validation error, got: %v", err)
	}
}

func TestCLIPruneNoRetentionNamed(t *testing.T) {
	cfgPath := testCLIEnv(t, "plans:\n"+validPlan("alpha", t.TempDir()))
	err := runCLI(t, "prune", "-c", cfgPath, "alpha")
	if err == nil || !strings.Contains(err.Error(), "no retention policy") {
		t.Fatalf("expected no-retention error, got: %v", err)
	}
}

// Pruning all plans skips plans without a retention policy instead of
// failing the whole command.
func TestCLIPruneAllSkipsNoRetention(t *testing.T) {
	cfgPath := testCLIEnv(t, "plans:\n"+validPlan("alpha", t.TempDir()))
	out, err := runCLIOut(t, "prune", "-c", cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no retention policy, skipping") {
		t.Errorf("expected skip message, got: %s", out)
	}
}

func TestCLIVerifyUnknownPlan(t *testing.T) {
	cfgPath := testCLIEnv(t, "plans:\n"+validPlan("alpha", t.TempDir()))
	err := runCLI(t, "verify", "-c", cfgPath, "nope")
	if err == nil || !strings.Contains(err.Error(), "available plans") {
		t.Fatalf("expected available-plans error, got: %v", err)
	}
}

func TestCLIHistoryAndStatus(t *testing.T) {
	cfgPath := testCLIEnv(t, "plans:\n"+validPlan("alpha", t.TempDir()))

	// Record snapshots directly through the state store the commands use.
	store, err := state.New(defaultStatePath())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := store.RecordSnapshot(config.Snapshot{
			ID:   "snap-" + string(rune('a'+i)),
			Plan: "alpha",
			Size: int64(i + 1),
		}); err != nil {
			t.Fatal(err)
		}
	}
	store.Close()

	hist, err := runCLIOut(t, "history", "-c", cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hist, "snap-a") || !strings.Contains(hist, "snap-b") {
		t.Errorf("history should list both snapshots, got: %s", hist)
	}

	stat, err := runCLIOut(t, "status", "-c", cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stat, "alpha") || !strings.Contains(stat, "last=") {
		t.Errorf("status should show last run, got: %s", stat)
	}
}

func TestCLIHistoryNoSnapshots(t *testing.T) {
	cfgPath := testCLIEnv(t, "plans:\n"+validPlan("alpha", t.TempDir()))
	out, err := runCLIOut(t, "history", "-c", cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no snapshots") {
		t.Errorf("expected no-snapshots message, got: %s", out)
	}
}

// TestCLIRunEndToEnd exercises the whole wiring - config loading, plan
// selection, state, storage, engine run, and output - against a real
// S3-compatible endpoint. Skipped unless BACKUPD_TEST_MINIO=1, mirroring
// the storage integration tests.
func TestCLIRunEndToEnd(t *testing.T) {
	if os.Getenv("BACKUPD_TEST_MINIO") != "1" {
		t.Skip("set BACKUPD_TEST_MINIO=1 with a running MinIO to run the end-to-end CLI test")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "data")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "hello.txt"), []byte("cli e2e"), 0o644); err != nil {
		t.Fatal(err)
	}

	endpoint := testEnvOr("BACKUPD_TEST_MINIO_ENDPOINT", "localhost:9000")
	bucket := testEnvOr("BACKUPD_TEST_MINIO_BUCKET", "backupd-cli-test")

	cfgYAML := "plans:\n" +
		"  - name: cli-e2e\n" +
		"    sources:\n" +
		"      - type: file\n" +
		"        path: " + src + "\n" +
		"    destination:\n" +
		"      type: s3\n" +
		"      bucket: " + bucket + "\n" +
		"      prefix: /cli\n" +
		"      endpoint: " + endpoint + "\n" +
		"      region: us-east-1\n" +
		"      access-key: " + testEnvOr("BACKUPD_TEST_MINIO_ACCESS_KEY", "testuser") + "\n" +
		"      secret-key: " + testEnvOr("BACKUPD_TEST_MINIO_SECRET_KEY", "testpass123") + "\n" +
		"      secure: false\n"

	cfgPath := testCLIEnv(t, cfgYAML)

	out, err := runCLIOut(t, "run", "-c", cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "snapshot") || !strings.Contains(out, "complete") {
		t.Errorf("run should report the snapshot, got: %s", out)
	}

	// Unchanged rerun must skip the snapshot.
	out, err = runCLIOut(t, "run", "-c", cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "nothing changed") {
		t.Errorf("unchanged run should report nothing changed, got: %s", out)
	}

	// Verify through the same wiring.
	out, err = runCLIOut(t, "verify", "-c", cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "verification passed") {
		t.Errorf("verify should pass, got: %s", out)
	}
}

func testEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
