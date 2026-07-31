# backupd

The declarative S3-compatible backup daemon for Linux

**backupd** lets you define backup plans in a single YAML file — sources, destination, encryption,
retention, tags, and hooks — and handles the rest. Incremental by default, encrypted at rest, and
scheduled without extra tooling.

!!! warning "Not production ready"
    backupd is under active development. APIs, config schema, and snapshot formats may change
    without notice. Do not rely on it for critical backups yet.

## Key Features

- 🚀 **Incremental by Default** — rsync-style delta algorithm uploads only changed blocks, keeping repeated backups cheap on large datasets.
- 📦 **Zero Runtime Dependencies** — a single static Go binary; no agent, no database, no interpreter.
- 🔐 **Encrypted at Rest** — AES-256-GCM encryption with Argon2id key derivation; nothing stored readable.
- 🗂️ **S3-Compatible Storage** — AWS S3, MinIO, DigitalOcean Spaces, Backblaze B2, and anything with an S3 API.
- 🧩 **Multi-Source** — file trees, PostgreSQL, MySQL, MongoDB, SQLite, Docker volumes, and Kubernetes PVCs in one plan.
- ⏰ **Built-In Scheduling** — embedded cron daemon, or export native systemd timers and services.
- 🧹 **Automatic Retention** — keep-last, daily, weekly, and monthly policies prune old snapshots for you.
- 🔔 **Hooks** — pre-backup, post-backup, and on-failure commands for Healthchecks.io pings, notifications, and more.
- ✅ **Verifiable** — snapshot integrity verification and restore from any snapshot, anytime.

## When Should I Use backupd?

- **Servers and VPSes** — if you need reliable backups of `/etc`, web roots, application data, or databases on a schedule, backupd replaces ad-hoc `cron` + `tar` + `scp` pipelines.
- **Databases** — if you need consistent logical dumps of PostgreSQL, MySQL, MongoDB, or SQLite — including the database *and* its files — in one place.
- **Container workloads** — if you run Docker volumes or Kubernetes PVCs and want their contents in the same backup stream as everything else.
- **Offsite storage** — if you want backups in S3-compatible object storage (MinIO at home, B2/Spaces/S3 in the cloud) with encryption applied before upload.
- **Notification and monitoring** — if you want Healthchecks.io pings, Slack/Discord alerts, or email on every success and failure via hooks.
- **Retention discipline** — if you want keep-last + daily + weekly + monthly pruning so backups never silently accumulate unbounded.

## Quick Example

```yaml
# ~/.backupd.yaml
plans:
  - name: server
    schedule: "0 3 * * *"          # every day at 03:00
    sources:
      - type: file
        path: /etc
        exclude: ["*.log", ".cache"]
      - type: database
        adapter: postgres
        dsn: "postgres://user:pass@localhost:5432/mydb"
    destination:
      type: s3
      bucket: my-backups
      endpoint: s3.amazonaws.com
      region: us-east-1
      access-key: ${AWS_ACCESS_KEY_ID}
      secret-key: ${AWS_SECRET_ACCESS_KEY}
    encryption:
      passphrase: ${BACKUP_PASSPHRASE}
    retention:
      keep-last: 30
      keep-daily: 7
      keep-weekly: 4
```

```shell
backupd run server                # first run backs everything up
backupd history server            # watch snapshots accumulate
backupd restore server <id> --target /tmp/restore   # restore any snapshot
```

## Next Steps

- [Installation](installation.md) — get the binary
- [Quick Start](quickstart.md) — your first backup in five minutes
- [Configuration](configuration.md) — full config reference
- [Examples](examples.md) — real-world setups for servers, databases, and containers
