package retention

import (
	"fmt"
	"sort"
	"time"

	"github.com/xero/backupd/internal/config"
)

type Policy struct {
	KeepLast    int
	KeepDaily   int
	KeepWeekly  int
	KeepMonthly int
}

func FromConfig(r *config.Retention) Policy {
	if r == nil {
		return Policy{}
	}
	return Policy{
		KeepLast:    r.KeepLast,
		KeepDaily:   r.KeepDaily,
		KeepWeekly:  r.KeepWeekly,
		KeepMonthly: r.KeepMonthly,
	}
}

type SnapshotSummary struct {
	ID        string
	Timestamp time.Time
	Size      int64
}

func (p Policy) Evaluate(snapshots []SnapshotSummary) (keep, delete []SnapshotSummary) {
	if len(snapshots) == 0 {
		return nil, nil
	}

	sorted := make([]SnapshotSummary, len(snapshots))
	copy(sorted, snapshots)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.After(sorted[j].Timestamp)
	})

	kept := make(map[string]bool)

	if p.KeepLast > 0 {
		for i := 0; i < p.KeepLast && i < len(sorted); i++ {
			kept[sorted[i].ID] = true
		}
	}

	now := time.Now().UTC()

	if p.KeepDaily > 0 {
		p.keepByBucket(sorted, kept, func(t time.Time) string {
			return t.Format("2006-01-02")
		}, p.KeepDaily, now)
	}

	if p.KeepWeekly > 0 {
		p.keepByBucket(sorted, kept, func(t time.Time) string {
			year, week := t.ISOWeek()
			return fmt.Sprintf("%d-W%02d", year, week)
		}, p.KeepWeekly, now)
	}

	if p.KeepMonthly > 0 {
		p.keepByBucket(sorted, kept, func(t time.Time) string {
			return t.Format("2006-01")
		}, p.KeepMonthly, now)
	}

	for _, s := range sorted {
		if kept[s.ID] {
			keep = append(keep, s)
		} else {
			delete = append(delete, s)
		}
	}

	return keep, delete
}

func (p Policy) keepByBucket(sorted []SnapshotSummary, kept map[string]bool, bucket func(time.Time) string, maxCount int, now time.Time) {
	buckets := make(map[string][]SnapshotSummary)

	for _, s := range sorted {
		key := bucket(s.Timestamp)
		buckets[key] = append(buckets[key], s)
	}

	var keys []string
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] > keys[j]
	})

	count := 0
	for _, key := range keys {
		if count >= maxCount {
			break
		}
		ss := buckets[key]
		if len(ss) > 0 {
			kept[ss[0].ID] = true
			count++
		}
	}
}
