# Encryption

```yaml
encryption:
  passphrase: ${BACKUP_PASSPHRASE}
```

## What is encrypted

With a passphrase set, **all content** is encrypted before it reaches the bucket:

- **Dedup blocks** (file contents) - each 8 KiB block individually, AES-256-GCM, convergent
  encryption (see below).
- **Source archives** (Docker volumes, Kubernetes PVCs) - streamed as 1 MiB
  AES-256-GCM frames and stored with a `.enc` suffix.

The snapshot manifest (paths, sizes, hashes) stays plaintext so incremental runs, `history`, and
`status` work without a passphrase - it contains metadata, never content.

## How it works

- The passphrase is derived into a 256-bit key using **Argon2id**. The salt is generated on the
  first run and reused for the plan's lifetime (loaded from the most recent manifest), so the
  same passphrase always yields the same key for a plan - required for cross-snapshot block
  deduplication. Changing the passphrase changes the key and therefore the ciphertext.
- **Blocks** use convergent AES-256-GCM: the block's key and nonce are derived from the master
  key and the block's plaintext hash, and that hash is bound as authenticated data. Encryption is
  deterministic - the same content always encrypts to the same ciphertext, so deduplication works
  on encrypted data while a different master key still yields unreadable ciphertext.
- **Archives** are encrypted with a random nonce per stream in 1 MiB AEAD frames, which keeps
  memory use flat no matter how large the dump is.
- **Database dumps** are stored as blocks like files, so they are encrypted with the same
  convergent block scheme - an unchanged database uploads nothing even on an encrypted plan.
- On restore and `verify`, every block is decrypted and its plaintext hash re-checked, and every
  archive stream is decrypted end-to-end - tampering or partial loss is detected, not silently
  restored.

!!! warning "Keep the passphrase safe"
    There is no recovery mechanism. If you lose the passphrase, the encrypted snapshots are
    unrecoverable. Store it in a password manager or your environment file.

## Restoring encrypted snapshots

`backupd restore` and `backupd verify` read the passphrase from the plan's `encryption.passphrase`
(including `${ENV}` interpolation), so restoration works with the same config used to create the
backup. If a snapshot is encrypted and the plan has no passphrase, restore and verify refuse to
run.

## Changing the passphrase

Each snapshot is encrypted with the passphrase that was configured at the time of its run (the
plan's salt is stable, so the key depends only on the passphrase). Changing
`encryption.passphrase` affects new snapshots only - to restore
older snapshots, temporarily set the config back to the passphrase that was active when they were
created. Keep old passphrases recorded until you are done restoring old snapshots.
