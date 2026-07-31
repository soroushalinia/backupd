# CLI Reference

```
backupd [--config <path>] <command>
```

Global flag: `--config` / `-c` - path to config file (default `~/.backupd.yaml`).

## Commands

| Command | Description |
|---------|-------------|
| `list` | List configured plans |
| `status [plan]` | Show the last backup status for each plan (or one plan) |
| `history <plan>` | Show all snapshots for a plan |
| `run <plan>` | Execute a backup now |
| `run <plan> --dry-run` | Report what would be uploaded without writing anything |
| `check` | Validate the configuration and print a plan summary |
| `restore <plan> <id>` | Restore a snapshot to the current directory |
| `restore <plan> <id> --dry-run` | Report what would be restored without writing anything |
| `prune <plan>` | Apply the plan's retention policy now, then garbage-collect orphaned blocks |
| `prune <plan> --dry-run` | Report what would be deleted without deleting anything |
| `daemon` | Run the scheduler daemon |
| `verify <plan> [id]` | Verify snapshot integrity (all snapshots, or one) |
| `export-systemd [plan]` | Generate systemd timer + service units |
| `completion <shell>` | Generate shell completion (`bash`, `zsh`, `fish`) |
| `help` | Show help |

## check

```shell
backupd check
```

Validates the whole configuration and prints a summary per plan: sources, destination, schedule,
encryption, and retention. Problems are reported as warnings and make `check` exit non-zero:

- file source paths that are not accessible
- cron schedules that do not parse
- encryption enabled without a passphrase

## run

```shell
backupd run server               # execute the plan
backupd run server --dry-run     # show what would be uploaded
```

With `--dry-run` no data is written: storage uploads, hooks, snapshot state, and retention pruning
are all skipped. The reported size is what a real run would upload.

## export-systemd

```shell
backupd export-systemd server -o /etc/systemd/system
```

| Flag      | Shorthand | Description |
|-----------|-----------|-------------|
| `--output`| `-o`      | Output directory for the generated unit files (default: current directory) |
| `--binary`|           | Path to the backupd binary used in the service unit (default: resolved from `os.Executable()`) |
| `--config`|           | Path to config file (overrides the global flag for unit generation) |

## restore

```shell
backupd restore server <snapshot-id> --target /tmp/restore
backupd restore server <snapshot-id> --dry-run   # report without writing
```

- `--target` / `-t` - directory to restore into (defaults to the current directory)
- `--dry-run` - read the manifest and report each source (file count, bytes, blocks) and whether
  archive objects are present, without writing anything
- File sources are reconstructed block-by-block from the manifest; other sources are written as
  their stored archive (`.sql`, `.tar`).

## prune

```shell
backupd prune server               # apply retention now
backupd prune server --dry-run     # report what would be deleted
```

Pruning normally happens automatically after every successful backup run; `prune` applies the
plan's retention policy on demand, deleting the snapshots the policy no longer keeps and then
garbage-collecting orphaned blocks. With `--dry-run` nothing is deleted - the snapshots and
orphaned blocks that would be removed are reported.

## verify

```shell
backupd verify server          # verify all snapshots
backupd verify server <id>     # verify one snapshot
```

Checks that every object referenced by a snapshot's manifest exists and decrypts successfully.

## exit codes

- `0` - success
- non-zero - error (missing args, config errors, backup/restore failures)
