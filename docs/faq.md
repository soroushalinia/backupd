# FAQ

## Is backupd production ready?

No. It is under active development — config schema and snapshot formats may change without
notice. Use it for testing and feedback, not for critical backups yet.

## Do I need to keep the daemon running?

Only if you want scheduled backups. Options:

- `backupd daemon` — embedded scheduler, keeps running in the foreground
- `backupd export-systemd` — generate systemd units and let the OS handle scheduling
- Neither — run `backupd run <plan>` from your own `cron` if you prefer

## How does the delta algorithm work?

Each file is split into fixed-size blocks with SHA-256 hashes. The hash of every block is
compared against the previous snapshot's manifest: unchanged blocks are skipped, and only new or
changed blocks are uploaded. See [Sources](sources.md#delta-algorithm).

## What happens if I lose the encryption passphrase?

Your snapshots are unrecoverable. AES-256-GCM keys are derived from the passphrase with Argon2id
and the passphrase is never stored anywhere. Store it in a password manager.

## Can I restore to a different machine?

Yes. Restore only needs network access to the bucket and the plan's passphrase — `backupd
restore` downloads the manifest and reconstructs the data locally. The config file is only used
to locate the destination and read the passphrase.

## Which storage services work?

Anything with an S3-compatible API: AWS S3, MinIO, DigitalOcean Spaces, Backblaze B2, Wasabi,
Scaleway, and more. See [Configuration](configuration.md#destination).

## Why are database dumps stored separately from files?

Each source is backed up independently — database dumps as `.sql`/`.dump` streams, files as delta
block objects, Docker/Kubernetes as `.tar`. The snapshot manifest records all of them, so one
snapshot restores every source of the plan.

## How is this different from restic, borg, or kopia?

Those are general-purpose deduplicating backup tools with their own repository formats. backupd
is a declarative daemon focused on S3-compatible object storage: YAML plans, database and
container sources, retention, hooks, and scheduling out of the box, with objects stored as plain
structured keys that are inspectable in the bucket.

## Does backupd upload to the bucket on every run?

File sources upload only changed blocks (incremental). Database, Docker, and Kubernetes sources
are dumped fresh each run — that's how consistency is guaranteed for those source types.

## How do I see the last backup status?

`backupd status` prints the last run result per plan; `backupd history <plan>` lists all
snapshots. Snapshots also carry any `tags` you defined in the plan.
