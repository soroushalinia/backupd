# Changelog

## v0.3.0 - unreleased

### Features

- **`run`, `prune`, `verify`, and `history` accept no plan name**: without an argument they
  operate on every configured plan (matching `status`), and an unknown plan name now errors with
  the list of available plans instead of a bare "accepts 1 arg(s)".
- **No-change runs record no snapshot**: a run whose files and dumps are identical to the
  previous snapshot reports `nothing changed` and skips the empty history row (new empty files
  and deletions still count as changes).

### Fixes

- **Unencrypted backups are now integrity-verified**: `verify` and `restore` check every block
  against the content hash recorded in its manifest, not just encrypted ones - tampered or
  corrupted storage is detected in both modes.
- **Restore never leaves partial files**: files and dumps are written to a temp file and renamed
  into place only after every block downloads and verifies, so a corrupt block cannot produce a
  silently truncated file.

### Tests

- CLI command wiring: list/check/run/prune/verify/history through the real cobra root command,
  plan-selection errors, and an end-to-end run-verify cycle against MinIO (gated on
  `BACKUPD_TEST_MINIO=1`).
- Corruption detection for encrypted and unencrypted backups (verify and restore), S3
  multipart/exists round trips against MinIO, progress reader, plan selection.

## v0.2.0 - 2026-07-31

### Features

- **Hardlinks**: detected during the file walk (device + inode), stored once (first path carries
  the blocks, others record a link reference), and restored as real hardlinks - the link
  relationship survives a restore.
- **Doublestar exclude patterns**: gitignore-style globs (`**`, `*`, `?`, `{a,b}`) matched against
  the full relative path, the basename, and any path segment. Substring matching is gone (`cache`
  does not match `cachedir`).
- **Streaming multipart uploads** for container archives: docker and kubernetes dumps stream as
  8 MiB multipart parts (encrypted in flight), no spooling to temporary files, aborted cleanly on
  failure. Storages without multipart support fall back to the spool path.
- **`backupd prune <plan>`**: applies the plan's retention policy on demand, then
  garbage-collects orphaned blocks. `--dry-run` reports what would be deleted without deleting.
- **`restore --dry-run`**: reads the manifest and reports each source (file count, bytes, blocks)
  and whether archive objects are present, without writing anything.
- **Per-file upload logging**: each changed file reports the bytes actually uploaded and its block
  count; unchanged files log as such; plans with a rate limit announce it up front.

### Fixes

- **Prune could corrupt kept snapshots**: `collectBlocks` ignored source-level database dump
  blocks, so pruning deleted blocks still referenced by kept snapshots. Fixed, and manifest parse
  errors now propagate instead of silently skipping.
- **Prune never deleted source archives** of expired snapshots (`deleteSnapshot` double-prefixed
  keys).
- **Orphaned blocks now reclaimed immediately**: garbage collection runs after failed runs and
  on plans without a retention policy, not only during retention pruning.
- **Failing capture tools now fail the run**: a database dump or container capture that exits
  nonzero mid-stream produced a truncated snapshot that was stored and verified as healthy; the
  exit status is now checked and the run aborts (uploaded objects are cleaned up).
- **Stable encryption salt**: the per-plan salt is loaded from the most recent manifest instead
  of being regenerated, so encrypted incremental snapshots verify.

### Tests

- Large-file round trip (96 MiB, ~12k content-addressed blocks), engine-level traversal
  rejection for malicious manifests (file paths and hardlink targets), failed runs with plain
  file sources (no docker required), multipart round trip, dump-tool failure handling, hardlink
  detection/restore, database dump dedup across runs.

## v0.1.0 - 2026-07-31

First release.

- Incremental delta backups: files are hashed once per run, unchanged files upload nothing,
  changed files split into 8 KiB content-addressed blocks (uploaded only when the object does not
  already exist in the bucket).
- Database dumps go through the same block pipeline - an unchanged database uploads only the
  manifest.
- Convergent AES-256-GCM encryption for blocks and stream-encrypted archives (1 MiB frames),
  Argon2id key derivation, stable per-plan salt.
- Retention policies (keep-last / keep-daily / keep-weekly / keep-monthly) with automatic
  pruning and orphaned-block garbage collection.
- Container sources: docker volumes and kubernetes PVCs snapshotted as archives.
- Symlinks and empty directories backed up; restore paths contained under the target directory.
- On-demand and scheduled runs (`backupd daemon`, systemd export), pre/post/on-failure hooks,
  per-plan rate limiting, `backupd check` and `run --dry-run`.
- Failed runs clean up their partial objects; snapshot state is recorded only on success.
