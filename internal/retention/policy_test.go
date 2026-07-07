package retention

import (
	"testing"
	"time"

	"github.com/soroushalinia/backupd/internal/config"
)

func mustTime(s string) time.Time {
	t, err := time.Parse("2006-01-02T15:04:05", s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func TestKeepLast(t *testing.T) {
	policy := Policy{KeepLast: 2}
	snapshots := []SnapshotSummary{
		{ID: "a", Timestamp: mustTime("2026-01-03T00:00:00")},
		{ID: "b", Timestamp: mustTime("2026-01-02T00:00:00")},
		{ID: "c", Timestamp: mustTime("2026-01-01T00:00:00")},
	}

	keep, del := policy.Evaluate(snapshots)
	if len(keep) != 2 {
		t.Fatalf("expected 2 kept, got %d", len(keep))
	}
	if len(del) != 1 {
		t.Fatalf("expected 1 deleted, got %d", len(del))
	}
	if keep[0].ID != "a" || keep[1].ID != "b" {
		t.Errorf("kept wrong snapshots: %v", keep)
	}
	if del[0].ID != "c" {
		t.Errorf("deleted wrong snapshot: %s", del[0].ID)
	}
}

func TestKeepLastZero(t *testing.T) {
	policy := Policy{}
	snapshots := []SnapshotSummary{
		{ID: "a", Timestamp: mustTime("2026-01-01T00:00:00")},
	}

	keep, del := policy.Evaluate(snapshots)
	if len(keep) != 0 {
		t.Errorf("expected 0 kept, got %d", len(keep))
	}
	if len(del) != 1 {
		t.Errorf("expected 1 deleted, got %d", len(del))
	}
}

func TestKeepDaily(t *testing.T) {
	policy := Policy{KeepDaily: 2}
	snapshots := []SnapshotSummary{
		{ID: "d1", Timestamp: mustTime("2026-01-03T12:00:00")},
		{ID: "d2", Timestamp: mustTime("2026-01-03T06:00:00")},
		{ID: "d3", Timestamp: mustTime("2026-01-02T12:00:00")},
		{ID: "d4", Timestamp: mustTime("2026-01-01T12:00:00")},
	}

	keep, _ := policy.Evaluate(snapshots)
	if len(keep) != 2 {
		t.Fatalf("expected 2 kept, got %d: %v", len(keep), keep)
	}
	// should keep d1 (latest on 01-03) and d3 (latest on 01-02)
	if keep[0].ID != "d1" || keep[1].ID != "d3" {
		t.Errorf("kept wrong: %v", keep)
	}
}

func TestKeepWeekly(t *testing.T) {
	// ISO week: 2026-01-05 is Monday of week 2
	policy := Policy{KeepWeekly: 2}
	snapshots := []SnapshotSummary{
		{ID: "w1", Timestamp: mustTime("2026-01-05T12:00:00")}, // week 2
		{ID: "w2", Timestamp: mustTime("2026-01-04T12:00:00")}, // week 1 (Sunday)
		{ID: "w3", Timestamp: mustTime("2026-01-03T12:00:00")}, // week 1
	}

	keep, _ := policy.Evaluate(snapshots)
	// should keep latest from each of 2 most recent weeks: w1 (week 2), w2 (week 1)
	if len(keep) != 2 {
		t.Fatalf("expected 2 kept, got %d: %v", len(keep), keep)
	}
	if keep[0].ID != "w1" || keep[1].ID != "w2" {
		t.Errorf("kept wrong snapshots: %v", keep)
	}
}

func TestKeepMonthly(t *testing.T) {
	policy := Policy{KeepMonthly: 2}
	snapshots := []SnapshotSummary{
		{ID: "m1", Timestamp: mustTime("2026-03-15T00:00:00")},
		{ID: "m2", Timestamp: mustTime("2026-02-10T00:00:00")},
		{ID: "m3", Timestamp: mustTime("2026-01-05T00:00:00")},
	}

	keep, _ := policy.Evaluate(snapshots)
	if len(keep) != 2 {
		t.Fatalf("expected 2 kept, got %d", len(keep))
	}
}

func TestKeepLastAndDaily(t *testing.T) {
	// KeepDaily 1 = keep 1 snapshot from the latest 1 day
	// KeepLast 1 = keep the single latest snapshot
	// Union = keep 'a' only
	policy := Policy{KeepLast: 1, KeepDaily: 1}
	snapshots := []SnapshotSummary{
		{ID: "a", Timestamp: mustTime("2026-01-03T12:00:00")},
		{ID: "b", Timestamp: mustTime("2026-01-02T12:00:00")},
		{ID: "c", Timestamp: mustTime("2026-01-01T12:00:00")},
	}

	keep, del := policy.Evaluate(snapshots)
	if len(keep) != 1 {
		t.Fatalf("expected 1 kept, got %d: %v", len(keep), keep)
	}
	if keep[0].ID != "a" {
		t.Errorf("expected 'a' kept, got %s", keep[0].ID)
	}
	if len(del) != 2 {
		t.Fatalf("expected 2 deleted, got %d", len(del))
	}
}

func TestEmptySnapshots(t *testing.T) {
	policy := Policy{KeepLast: 10}
	keep, del := policy.Evaluate(nil)
	if keep != nil || del != nil {
		t.Fatal("expected nil for empty input")
	}
}

func TestFromConfig(t *testing.T) {
	r := &config.Retention{
		KeepLast:    7,
		KeepDaily:   7,
		KeepWeekly:  4,
		KeepMonthly: 12,
	}
	p := FromConfig(r)
	if p.KeepLast != 7 || p.KeepDaily != 7 || p.KeepWeekly != 4 || p.KeepMonthly != 12 {
		t.Errorf("FromConfig produced wrong policy: %+v", p)
	}
}

func TestFromConfigNil(t *testing.T) {
	p := FromConfig(nil)
	if p.KeepLast != 0 {
		t.Error("expected zero policy from nil config")
	}
}
