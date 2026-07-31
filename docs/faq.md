# FAQ

## Is backupd production ready?

No. It is under active development - config schema and snapshot formats may change without
notice. Use it for testing and feedback, not for critical backups yet.

## Do I need to keep the daemon running?

Only if you want scheduled backups. Options:

- `backupd daemon` - embedded scheduler, keeps running in the foreground
- `backupd export-systemd` - generate systemd units and let the OS handle scheduling
- Neither - run `backupd run <plan>` from your own `cron` if you prefer

## How does the delta algorithm work?

Every file is hashed once on each run. Unchanged files (same hash as the previous snapshot's
manifest) reuse their previous block references - zero uploads. Changed files are split into
fixed-size 8 KiB blocks; each block is uploaded only if it does not already exist in the bucket.
Blocks are content-addressed, so identical content anywhere in a plan shares one object, even
when encrypted. See [How It Works](architecture.md#incremental-backups) and
[Sources](sources.md#delta-algorithm).

## What happens if a backup fails partway through?

The run aborts, the on-failure hook runs, and no snapshot is recorded in `history`. The source
archives and manifest already uploaded for that run are deleted again; deduplication blocks are
left in place (they may be shared with good snapshots) and blocks orphaned by the failed run are
garbage-collected immediately. A capture tool that exits nonzero (for example a failing database
dump) also fails the run - a partial dump is never stored as a snapshot. See
[How It Works](architecture.md#failed-runs).

## How are symlinks and empty directories backed up?

Symlinks are stored as their target path, never followed - broken links are fine, and a link can
never pull content from outside the backup root. Empty directories and directory permissions are
preserved. On restore, directories are created first, then files, then symlinks, then hardlinks,
so nothing is written through a restored symlink. Hardlinks are detected during the walk and
stored once - the first path for an inode carries the blocks, the rest record a link reference -
and restored as real links. See [Sources](sources.md#file).

## What do rate limits do?

`rate-limit: 10M` on a plan throttles all transfers - uploads, restores, and verification - to
that many bytes per second (with a one-second burst). Useful for keeping backups from saturating
a link. See [Configuration](configuration.md#plan).

## How do I test a config or a backup without consequences?

`backupd check` validates the config and prints a per-plan summary with warnings; `backupd run
<plan> --dry-run` runs the whole pipeline against a read-only storage - nothing is uploaded, no
hooks run, no snapshot is recorded. `backupd prune <plan> --dry-run` and `backupd restore <plan>
<snapshot-id> --dry-run` do the same for deleting old snapshots and writing files, respectively.
See the [CLI Reference](cli.md#check).

## What happens if I lose the encryption passphrase?

Your snapshots are unrecoverable. AES-256-GCM keys are derived from the passphrase with Argon2id
and the passphrase is never stored anywhere. Store it in a password manager.

## Can I restore to a different machine?

Yes. Restore only needs network access to the bucket and the plan's passphrase - `backupd
restore` downloads the manifest and reconstructs the data locally. The config file is only used
to locate the destination and read the passphrase.

## Which storage services work?

Anything with an S3-compatible API: AWS S3, MinIO, DigitalOcean Spaces, Backblaze B2, Wasabi,
Scaleway, and more. See [Configuration](configuration.md#destination).

## Why are database dumps stored separately from files?

Each source is backed up independently - database dumps as block streams, files as delta
block objects, Docker/Kubernetes as `.tar`. The snapshot manifest records all of them, so one
snapshot restores every source of the plan.

## How is this different from restic, borg, or kopia?

Those are general-purpose deduplicating backup tools with their own repository formats. backupd
is a declarative daemon focused on S3-compatible object storage: YAML plans, database and
container sources, retention, hooks, and scheduling out of the box, with objects stored as plain
structured keys that are inspectable in the bucket.

## Does backupd upload to the bucket on every run?

File sources upload only changed blocks: unchanged files are skipped wholesale by comparing their
hash to the previous manifest (incremental), and even changed blocks are uploaded only when the
object isn't already in the bucket (dedup). Database dumps go through the same block pipeline, so
an unchanged database uploads only the manifest. A backup of a large, unchanged dataset uploads
just the manifest. Docker and Kubernetes sources are dumped fresh each run - that's how
consistency is guaranteed for those source types.

## How do I see the last backup status?

`backupd status` prints the last run result per plan; `backupd history <plan>` lists all
snapshots. Snapshots also carry any `tags` you defined in the plan.
