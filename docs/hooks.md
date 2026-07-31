# Hooks

```yaml
hooks:
  pre-backup:
    - "echo 'Starting backup...'"
  post-backup:
    - "curl -fsS -m 10 --retry 5 -o /dev/null https://hc-ping.com/my-uuid"
  on-failure:
    - "mail -s 'Backup failed' admin@example.com"
```

Hooks run shell commands at points in the backup lifecycle:

| Hook         | When |
|--------------|------|
| `pre-backup` | Before any source is processed |
| `post-backup`| After a successful backup, before retention pruning |
| `on-failure` | When the backup fails at any point |

## Behaviour

- Commands run via `/bin/sh -c` in order, and the plan aborts if a `pre-backup` hook fails.
- `post-backup` and `on-failure` hooks run but their own failures are logged, not fatal.

## Typical uses

- **Healthchecks.io / Uptime Kuma pings** — notify external monitors of success or failure
- **Notifications** — `mail`, `ntfy`, Slack/Discord webhooks via `curl`
- **Warm-up** — flush caches or rotate logs before the backup snapshot
