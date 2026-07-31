# Sources

A plan can have any number of sources. Each source is one of four types: `file`, `database`,
`docker`, or `kubernetes`.

## File

```yaml
- type: file
  path: /var/www
  exclude: ["*.log", "cache/", "tmp/"]
```

| Field     | Type     | Required | Description |
|-----------|----------|----------|-------------|
| `type`    | string   | yes      | `file`      |
| `path`    | string   | yes      | Root directory to back up |
| `exclude` | list     | no       | Glob patterns to skip |

Files are backed up with the [delta algorithm](#delta-algorithm).

## Database

```yaml
- type: database
  adapter: postgres
  dsn: "postgres://user:pass@localhost:5432/mydb"
  dump-tool: pg_dump
```

| Field       | Type   | Required | Description |
|-------------|--------|----------|-------------|
| `type`      | string | yes      | `database`  |
| `adapter`   | string | yes      | `postgres`, `mysql`, `mongodb`, or `sqlite` |
| `dsn`       | string | yes      | Connection string for the dump tool |
| `dump-tool` | string | no       | Override the dump binary (default: `pg_dump`, `mysqldump`, `mongodump`, `sqlite3`) |

The dump is piped to storage as a `.sql` (or `.dump`) object — no intermediate file on disk.

## Docker

```yaml
- type: docker
  volume: myapp_data
```

| Field    | Type   | Required | Description |
|----------|--------|----------|-------------|
| `type`   | string | yes      | `docker`    |
| `volume` | string | yes      | Docker volume name |

The volume is snapshotted with `docker run --rm` and stored as a `.tar` object.

## Kubernetes

```yaml
- type: kubernetes
  pvc: data-pvc
  snapshot: true
```

| Field      | Type | Required | Description |
|------------|------|----------|-------------|
| `type`     | string | yes    | `kubernetes` |
| `pvc`      | string | yes    | PersistentVolumeClaim name |
| `snapshot` | bool  | no      | Use a CSI volume snapshot before backing up |

The PVC is copied via `kubectl exec` and stored as a `.tar` object.

## Delta algorithm

File sources use an rsync-style delta algorithm:

1. The source is walked and each file is split into fixed-size blocks.
2. Each block's SHA-256 hash is compared against the previous snapshot's manifest.
3. Only new or changed blocks are uploaded; identical blocks are skipped.
4. A new manifest records the file → block mapping, which restores reconstruct files from.

This keeps repeated backups cheap for large, mostly-unchanged datasets.
