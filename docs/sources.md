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

Directories - including empty ones - are backed up with their permissions. Symlinks are stored as
their target path instead of being followed, so broken links are backed up fine and a link can
never pull content from outside the source root. During restore, directories are created first,
then regular files, then symlinks, so nothing is ever written through a restored symlink. Special
files (fifos, sockets, devices) are skipped with a warning.

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

The dump is piped to storage as a `.sql` (or `.dump`) object - no intermediate file on disk.

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

1. The source is walked; directories and symlinks are stored as manifest entries, and each regular
   file is split into fixed-size blocks.
2. Each file is hashed once and compared against the previous snapshot's manifest.
3. Unchanged files reuse their previous block references; new or changed blocks are uploaded only
   when the object does not already exist in storage.
4. A new manifest records the file → block mapping, which restores reconstruct files from.

This keeps repeated backups cheap for large, mostly-unchanged datasets.
