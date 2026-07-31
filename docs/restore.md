# Restore & Verify

## Listing snapshots

```shell
backupd history server
```

Shows every snapshot for a plan with its timestamp, size, and source list. Pick the snapshot ID
you want to restore or verify.

## Restoring

```shell
backupd restore server <snapshot-id>
backupd restore server <snapshot-id> --target /tmp/restore
backupd restore server <snapshot-id> -t /tmp/restore   # shorthand
```

- File sources are reconstructed block-by-block from the delta manifest — you get the exact files
  as of that snapshot.
- Database, Docker, and Kubernetes sources are written as their stored archive (`.sql`, `.tar`)
  into the target directory, named after the source object.

!!! tip "Restore is read-only on the bucket"
    Restoring never modifies or deletes snapshot objects — it only downloads.

## Verifying integrity

```shell
backupd verify server          # verify all snapshots for the plan
backupd verify server <id>     # verify a single snapshot
```

`verify` checks that every object referenced by a snapshot's manifest exists in the bucket and
decrypts successfully with the plan's passphrase. Run it after every backup via a `post-backup`
hook to catch corruption early:

```yaml
hooks:
  post-backup:
    - "backupd verify {{ .Plan }}"
```

!!! note "Encrypted snapshots"
    Restore and verify use the passphrase from the plan's `encryption.passphrase` (including
    `${ENV}` interpolation), so they work with the same config used to create the backup.
