# Encryption

```yaml
encryption:
  passphrase: ${BACKUP_PASSPHRASE}
```

## How it works

- The passphrase is derived into a 256-bit key using **Argon2id**.
- Each snapshot object is encrypted with **AES-256-GCM**.
- Encrypted objects are stored with a `.enc` suffix and are unreadable without the passphrase.

!!! warning "Keep the passphrase safe"
    There is no recovery mechanism. If you lose the passphrase, the encrypted snapshots are
    unrecoverable. Store it in a password manager or your environment file.

## Restoring encrypted snapshots

`backupd restore` and `backupd verify` read the passphrase from the plan's `encryption.passphrase`
(including `${ENV}` interpolation), so restoration works with the same config used to create the
backup.
