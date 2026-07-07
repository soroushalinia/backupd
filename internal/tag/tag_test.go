package tag

import (
	"strings"
	"testing"
)

func TestReservedTags(t *testing.T) {
	tags := ReservedTags("myplan", "snap-123", "2026-01-01T00:00:00Z", 3)

	if tags["backupd:plan"] != "myplan" {
		t.Errorf("plan = %q, want %q", tags["backupd:plan"], "myplan")
	}
	if tags["backupd:snapshot"] != "snap-123" {
		t.Errorf("snapshot = %q, want %q", tags["backupd:snapshot"], "snap-123")
	}
	if tags["backupd:sources"] != "3" {
		t.Errorf("sources = %q, want %q", tags["backupd:sources"], "3")
	}
}

func TestMerge(t *testing.T) {
	base := map[string]string{"a": "1", "b": "2"}
	extra := map[string]string{"c": "3"}

	merged := Merge(base, extra)

	if len(merged) != 3 {
		t.Errorf("len = %d, want 3", len(merged))
	}
	if merged["a"] != "1" || merged["b"] != "2" || merged["c"] != "3" {
		t.Error("merge values incorrect")
	}
}

func TestMergeOverride(t *testing.T) {
	base := map[string]string{"a": "1"}
	extra := map[string]string{"a": "override"}

	merged := Merge(base, extra)
	if merged["a"] != "override" {
		t.Errorf("a = %q, want 'override'", merged["a"])
	}
}

func TestReservedTagsPrefix(t *testing.T) {
	tags := ReservedTags("x", "y", "z", 1)
	for k := range tags {
		if !strings.HasPrefix(k, "backupd:") {
			t.Errorf("tag key %q missing backupd: prefix", k)
		}
	}
}
