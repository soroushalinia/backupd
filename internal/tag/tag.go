package tag

import "fmt"

func ReservedTags(plan, snapshotID, timestamp string, sourceCount int) map[string]string {
	return map[string]string{
		"backupd:plan":      plan,
		"backupd:snapshot":  snapshotID,
		"backupd:timestamp": timestamp,
		"backupd:sources":   fmt.Sprintf("%d", sourceCount),
	}
}

func Merge(base, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}
