package scheduler

import (
	"strings"
	"testing"
)

func TestGenerateSystemd(t *testing.T) {
	units, err := GenerateSystemd("daily-backup", "0 2 * * *", "/usr/local/bin/backupd", "/etc/backupd.yaml")
	if err != nil {
		t.Fatalf("GenerateSystemd: %v", err)
	}

	if !strings.Contains(units.Service, "ExecStart=/usr/local/bin/backupd run daily-backup") {
		t.Errorf("service missing ExecStart: %s", units.Service)
	}

	if !strings.Contains(units.Service, "--config /etc/backupd.yaml") {
		t.Errorf("service missing --config: %s", units.Service)
	}

	if !strings.Contains(units.Timer, "OnCalendar=") {
		t.Errorf("timer missing OnCalendar: %s", units.Timer)
	}

	if !strings.Contains(units.Timer, "Persistent=true") {
		t.Errorf("timer missing Persistent: %s", units.Timer)
	}
}

func TestCronToSystemd(t *testing.T) {
	tests := []struct {
		cron string
		want string
	}{
		{"0 2 * * *", "*-*-* *-*-* 02:00:00"},
		{"30 1 * * 1", "Mon *-*-* 01:30:00"},
		{"*/5 * * * *", "*-*-* *-*-* *:*/5:00"},
	}

	for _, tt := range tests {
		got, err := cronToSystemd(tt.cron)
		if err != nil {
			t.Errorf("cronToSystemd(%q) error: %v", tt.cron, err)
			continue
		}
		if got != tt.want {
			t.Errorf("cronToSystemd(%q) = %q, want %q", tt.cron, got, tt.want)
		}
	}
}

func TestCronToSystemdInvalid(t *testing.T) {
	_, err := cronToSystemd("invalid")
	if err == nil {
		t.Error("expected error for invalid cron")
	}
}
