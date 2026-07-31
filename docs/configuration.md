# Configuration

The default config path is `~/.backupd.yaml` (override with `--config`). A config file contains a
top-level `plans` list.

```yaml
plans:
  - name: server-backup
    schedule: "0 3 * * *"
    sources:
      - type: file
        path: /etc/nginx
    destination:
      type: s3
      bucket: my-backups
    encryption:
      passphrase: ${BACKUP_PASSPHRASE}
    retention:
      keep-last: 30
    tags:
      environment: production
    hooks:
      pre-backup:
        - "echo 'Starting backup...'"
```

## Plan

| Field        | Type   | Required | Description                              |
|--------------|--------|----------|------------------------------------------|
| `name`       | string | yes      | Unique plan name                         |
| `schedule`   | string | no       | Cron expression (e.g. `0 3 * * *`)       |
| `rate-limit` | string | no       | Max transfer rate in bytes/second (e.g. `10M`, `500K`); `0` = unlimited |
| `sources`    | list   | yes      | One or more sources (see [Sources](sources.md)) |
| `destination`| object | yes      | Storage destination (see below)          |
| `encryption` | object | no       | Encryption settings (see [Encryption](encryption.md)) |
| `retention`  | object | no       | Retention policy (see [Retention](retention.md)) |
| `tags`       | map    | no       | Key/value tags applied to snapshot objects |
| `hooks`      | object | no       | Pre/post/on-failure hooks (see [Hooks](hooks.md)) |

`rate-limit` throttles uploads and downloads for the plan — block transfers during backups and
restores. Suffixes `K`, `M`, `G` (and `KiB`/`MiB`/`GiB`) are powers of 1024; a bare integer is
bytes per second.

## Destination

| Field          | Type   | Required | Description |
|----------------|--------|----------|-------------|
| `type`         | string | yes      | `s3`        |
| `bucket`       | string | yes      | Bucket name |
| `prefix`       | string | no       | Key prefix, e.g. `/server` |
| `endpoint`     | string | no       | S3 endpoint; defaults to `s3.amazonaws.com` |
| `region`       | string | no       | AWS region, e.g. `us-east-1` |
| `access-key`   | string | yes      | Access key (`${ENV}` supported) |
| `secret-key`   | string | yes      | Secret key (`${ENV}` supported) |
| `storage-class`| string | no       | e.g. `STANDARD_IA`, `GLACIER` |
| `secure`       | bool   | no       | Use HTTPS; defaults to true, set `false` for plain HTTP endpoints (e.g. local MinIO) |

!!! note "Compatible services"
    Any S3-compatible service works: AWS S3, MinIO, DigitalOcean Spaces, Backblaze B2, Wasabi.

## Environment interpolation

Any `${VAR}` in the config is replaced with the value of the environment variable at load time.
Variables are read from the process environment — ideal for keys, passphrases, and DSN passwords.

## Validation

Config is validated at load time: missing required fields, unknown source types, and missing
destination settings fail fast with clear errors. Run `backupd check` for a deeper review — it
also reports inaccessible file source paths, invalid cron schedules, and encryption setups
without a passphrase.
