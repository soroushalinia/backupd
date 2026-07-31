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

Hardlinks are detected during the walk and stored once: the first path seen for an inode becomes
the canonical entry that carries the blocks, and every other path records a hardlink reference
(which uploads nothing). Restore recreates the links as real hardlinks, so the link relationship
survives a restore.

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

The dump is streamed straight into the block pipeline (see [Delta algorithm](#delta-algorithm)):
an unchanged database uploads nothing, and a changed one uploads only the blocks that differ -
even on encrypted plans. No intermediate file is written to disk.

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

File sources and database dumps use an rsync-style delta algorithm:

1. The source is walked; directories and symlinks are stored as manifest entries, and each regular
   file (or database dump stream) is split into fixed-size blocks.
2. Each file is hashed once and compared against the previous snapshot's manifest; database dumps
   reuse the previous dump's block references.
3. Unchanged files and unchanged dump blocks reuse their previous block references; new or changed
   blocks are uploaded only when the object does not already exist in storage.
4. A new manifest records the source → block mapping, which restores reconstruct files from.

This keeps repeated backups cheap for large, mostly-unchanged datasets.
