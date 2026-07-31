# Retention

```yaml
retention:
  keep-last: 30
  keep-daily: 7
  keep-weekly: 4
  keep-monthly: 12
```

Snapshots are pruned after every successful backup according to the policy. All four policies can
be combined; a snapshot is kept if any policy keeps it.

## Policies

| Field         | Description |
|---------------|-------------|
| `keep-last`   | Always keep the N most recent snapshots |
| `keep-daily`  | Keep N snapshots, one per day (most recent of each day) |
| `keep-weekly` | Keep N snapshots, one per week (most recent of each ISO week) |
| `keep-monthly`| Keep N snapshots, one per month (most recent of each month) |

Example — with the config above, backupd keeps:

- the 30 newest snapshots,
- plus the newest snapshot of each of the last 7 days,
- plus the newest snapshot of each of the last 4 weeks,
- plus the newest snapshot of each of the last 12 months.

Older snapshots and their objects are removed from the bucket. Empty parent prefixes are cleaned
up as well.

!!! note
    `keep-last` is the only policy that guarantees N snapshots exist. The calendar-based policies
    only prune when there is a newer snapshot in the same period.
