# backupd

[![Go Version](https://img.shields.io/github/go-mod/go-version/soroushalinia/backupd)](https://github.com/soroushalinia/backupd)
[![License](https://img.shields.io/github/license/soroushalinia/backupd)](LICENSE)
[![CI](https://github.com/soroushalinia/backupd/actions/workflows/ci.yml/badge.svg)](https://github.com/soroushalinia/backupd/actions)
[![Docs](https://img.shields.io/badge/docs-soroushalinia.github.io%2Fbackupd-blue)](https://soroushalinia.github.io/backupd/)
[![Release](https://img.shields.io/github/v/release/soroushalinia/backupd)](https://github.com/soroushalinia/backupd/releases)

> **Status: production ready for self-hosted use.** Stable config schema and snapshot format as of v0.2.0. Backups are integrity-verified (content hashes on every block, authenticated encryption when enabled) and covered by a race-clean test suite plus CI on Go 1.23/1.24 (linux/macOS) and a goreleaser-validated release pipeline.

Declarative S3-compatible backup daemon. Define your backup plans in a single YAML file - sources, destination, encryption, retention, tags, and hooks - and let backupd handle the rest.

Full documentation: <https://soroushalinia.github.io/backupd/>

Changelog: [CHANGELOG.md](CHANGELOG.md)

## Install

```shell
curl -fsSL https://raw.githubusercontent.com/soroushalinia/backupd/main/install.sh | sh
```

See the [installation guide](https://soroushalinia.github.io/backupd/installation/).

## Features

- Declarative YAML config with `${ENV_VAR}` interpolation
- S3-compatible storage (AWS S3, MinIO, DigitalOcean Spaces, Backblaze B2)
- Incremental backups via rsync-style delta algorithm
- Database backup: PostgreSQL, MySQL, MongoDB, SQLite (exec mode)
- Docker volume backup via `docker run --rm`
- Kubernetes PVC backup via `kubectl exec`
- AES-256-GCM encryption with Argon2id key derivation
- Retention policies: keep-last, daily, weekly, monthly
- Pre/post/on-failure command hooks
- Embedded cron scheduler and systemd timer/service export
- Snapshot integrity verification
- Shell completion for bash, zsh, fish

## Install

```shell
go install github.com/soroushalinia/backupd/cmd/backupd@latest
```

Or build from source:

```shell
git clone https://github.com/soroushalinia/backupd.git
cd backupd
make build
```

## Quick Start

Create `~/.backupd.yaml`:

```yaml
plans:
  - name: server
    schedule: "0 3 * * *"
    sources:
      - type: file
        path: /etc
        exclude: ["*.log", ".cache"]
    destination:
      type: s3
      bucket: my-backups
      endpoint: s3.amazonaws.com
      region: us-east-1
      access-key: ${AWS_ACCESS_KEY_ID}
      secret-key: ${AWS_SECRET_ACCESS_KEY}
    retention:
      keep-last: 30
      keep-daily: 7
      keep-weekly: 4
```

```shell
backupd run server
backupd status
backupd history server
backupd restore server <snapshot-id> --target /tmp/restore
```

## Commands

| Command | Description |
|---------|-------------|
| `list` | List configured plans |
| `status [plan]` | Show last backup status |
| `history [plan]` | Show all snapshots (or all plans) |
| `run [plan]` | Execute a backup (or all plans) |
| `restore <plan> <id>` | Restore a snapshot |
| `daemon` | Run the scheduler |
| `verify [plan] [id]` | Verify snapshot integrity |
| `export-systemd [plan]` | Generate systemd units |
| `completion <shell>` | Generate shell completions |

## License

MIT
