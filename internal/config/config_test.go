package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	content := `
plans:
  - name: test-plan
    schedule: "0 * * * *"
    sources:
      - type: file
        path: /tmp
    destination:
      type: s3
      bucket: test-bucket
      endpoint: s3.amazonaws.com
      region: us-east-1
      access-key: key
      secret-key: secret
`

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backupd.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(cfg.Plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(cfg.Plans))
	}

	if cfg.Plans[0].Name != "test-plan" {
		t.Errorf("plan name = %q, want %q", cfg.Plans[0].Name, "test-plan")
	}
}

func TestValidateInvalidSource(t *testing.T) {
	cfg := &Config{
		Plans: []Plan{
			{
				Name: "bad",
				Sources: []Source{
					{Type: "unknown"},
				},
				Destination: Destination{
					Type:     "s3",
					Bucket:   "b",
					Endpoint: "e",
				},
			},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for unknown source type")
	}
}

func TestValidateNoPlans(t *testing.T) {
	cfg := &Config{}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for empty plans")
	}
}

func TestLoadWithEnvVar(t *testing.T) {
	os.Setenv("TEST_BACKUP_BUCKET", "env-bucket")
	defer os.Unsetenv("TEST_BACKUP_BUCKET")

	content := `
plans:
  - name: env-test
    schedule: "0 * * * *"
    sources:
      - type: file
        path: /tmp
    destination:
      type: s3
      bucket: ${TEST_BACKUP_BUCKET}
      endpoint: s3.amazonaws.com
      region: us-east-1
      access-key: key
      secret-key: secret
`

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backupd.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Plans[0].Destination.Bucket != "env-bucket" {
		t.Errorf("bucket = %q, want %q", cfg.Plans[0].Destination.Bucket, "env-bucket")
	}
}
