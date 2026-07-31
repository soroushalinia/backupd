# CLI Reference

```
backupd [--config <path>] <command>
```

Global flag: `--config` / `-c` — path to config file (default `~/.backupd.yaml`).

## Commands

| Command | Description |
|---------|-------------|
| `list` | List configured plans |
| `status [plan]` | Show the last backup status for each plan (or one plan) |
| `history <plan>` | Show all snapshots for a plan |
| `run <plan>` | Execute a backup now |
| `restore <plan> <id>` | Restore a snapshot to the current directory |
| `daemon` | Run the scheduler daemon |
| `verify <plan> [id]` | Verify snapshot integrity (all snapshots, or one) |
| `export-systemd [plan]` | Generate systemd timer + service units |
| `completion <shell>` | Generate shell completion (`bash`, `zsh`, `fish`) |
| `help` | Show help |

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
```

- `--target` / `-t` — directory to restore into (defaults to the current directory)
- File sources are reconstructed block-by-block from the manifest; other sources are written as
  their stored archive (`.sql`, `.tar`).

## verify

```shell
backupd verify server          # verify all snapshots
backupd verify server <id>     # verify one snapshot
```

Checks that every object referenced by a snapshot's manifest exists and decrypts successfully.

## exit codes

- `0` — success
- non-zero — error (missing args, config errors, backup/restore failures)
