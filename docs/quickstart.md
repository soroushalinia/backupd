# Quick Start

Create a config file at `~/.backupd.yaml`:

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
```

Validate the config — it prints a per-plan summary and flags problems like missing paths or bad
schedules:

```shell
backupd check
```

Run a backup on demand (add `--dry-run` first to see what it would upload without writing
anything):

```shell
backupd run server --dry-run
backupd run server
```

Check the result:

```shell
backupd status
backupd history server
```

Restore a snapshot:

```shell
backupd restore server <snapshot-id> --target /tmp/restore
```

Run the daemon so the embedded scheduler executes plans on their cron schedule:

```shell
backupd daemon
```

Or export systemd units instead of running the daemon:

```shell
backupd export-systemd server
```

!!! tip "Environment variables"
    Values like `${AWS_ACCESS_KEY_ID}` are interpolated from the environment at load time, so
    secrets never have to live in the config file.
