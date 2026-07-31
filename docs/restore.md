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
  (and permissions) as of that snapshot. Directories are recreated first, then files, then
  symlinks, and empty directories are preserved.
- Symlinks are restored as their stored target and are never followed, so nothing is written
  through them during a restore.
- Database, Docker, and Kubernetes sources are written as their stored archive (`.sql`, `.tar`)
  into the target directory, named after the source object.
- Restores stream: files are written block-by-block and encrypted archives are decrypted
  chunk-by-chunk, so memory use stays flat regardless of file or dump size.

!!! tip "Restore is read-only on the bucket"
    Restoring never modifies or deletes snapshot objects — it only downloads.

### Path containment

Manifest paths are validated with `safeJoin`: every file is created only inside the target
directory, and a manifest that tries to escape (for example with `../`) is rejected with an
error. Restores of untrusted manifests cannot write outside the target.

### Integrity checks

While restoring, every encrypted block is decrypted and its plaintext hash is verified against
the manifest, and every encrypted archive is authenticated during decryption. Corrupt or
tampered data aborts the restore with an error — it is never silently written to disk.

## Verifying integrity

```shell
backupd verify server          # verify all snapshots for the plan
backupd verify server <id>     # verify a single snapshot
```

`verify` checks that every object referenced by a snapshot's manifest exists in the bucket and
decrypts successfully with the plan's passphrase: each dedup block is downloaded, decrypted, and
hash-checked, and each archive is stream-decrypted end-to-end. Run it after every backup via a
`post-backup` hook to catch corruption early:

```yaml
hooks:
  post-backup:
    - "backupd verify {{ .Plan }}"
```

!!! note "Encrypted snapshots"
    Restore and verify use the passphrase from the plan's `encryption.passphrase` (including
    `${ENV}` interpolation), so they work with the same config used to create the backup.
