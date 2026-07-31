# How It Works

This page explains what backupd actually stores in the bucket, how incremental backups work, and
how failures, retention, encryption, and restores behave.

## Storage layout

Every plan owns its own key space under a single bucket. All objects are plain S3 keys you can
inspect with any S3 client:

```
<plan>/
  blocks/
    <block-id>                         # deduplicated content-addressed blocks (files + database dumps)
  snapshots/
    <snapshot-uuid>/
      manifest.json                    # snapshot metadata + source -> block mapping
      sources/1.tar                    # docker volume / kubernetes PVC archive
```

The optional `destination.prefix` is prepended to every key.

## Deduplication blocks

File sources and database dumps are split into fixed-size blocks (8 KiB). Each block is stored as
a separate object, addressed by its content hash:

- **Unencrypted plan**: the object id is the SHA-256 of the plaintext block.
- **Encrypted plan**: the object id is the SHA-256 of the *ciphertext*.

Because the id is derived from the stored bytes, identical content always maps to the same object
no matter which file, database dump, plan, or snapshot references it - deduplication works across
sources, plans, and time.

## The snapshot manifest

Each run produces a manifest - a small JSON object that is the source of truth for everything
else:

```json
{
  "snapshot": "7f1c…",
  "plan": "server",
  "timestamp": "2026-07-31T12:00:00Z",
  "size": 123456789,
  "sources": [
    {
      "type": "file",
      "files": [
        {
          "path": "etc/nginx/nginx.conf",
          "size": 4096,
          "mode": 420,
          "link_target": "",
          "file_hash": "a3f1…",
          "blocks": [ { "id": "9c2e…", "hash": "b77a…" } ]
        }
      ]
    },
    { "type": "database", "key": "snapshots/7f1c…/sources/0.sql",
      "blocks": [ { "id": "9c2e…", "hash": "b77a…" } ] }
  ],
  "encryption": { "algorithm": "AES-256-GCM", "kdf": "Argon2id", "salt": "…" }
}
```

Each file entry records its path, size, permissions, and the ids of the blocks that make it up.
Directories, symlinks, and hardlinks are recorded as entries too (`link_target` holds a symlink
target; `hardlink_of` points at the canonical entry that carries the blocks).

!!! note "Manifest metadata is plaintext"
    File *contents* are always encrypted in encrypted plans, but the manifest itself is stored in
    plaintext: it contains paths, sizes, permissions, and hashes - not content. This is what lets
    incremental runs compare against the previous manifest and lets `history`/`status` work
    without a passphrase. If you need confidentiality for paths and names, keep them out of
    backup roots or encrypt at the storage layer.

## Incremental backups

Each run is cheap for unchanged data. The run reads the most recent snapshot's manifest and:

1. **Pass 1 - hash**: every regular file is streamed once through SHA-256. If its hash matches the
   previous manifest *and* the size is unchanged, the file's block references are copied from the
   previous manifest - zero bytes uploaded, zero blocks touched.
2. **Pass 2 - blocks**: for changed or new files, the file is streamed again, each block is
   hashed (and encrypted), and the object is uploaded only if it does not already exist in the
   bucket (`Exists` check). Repeated identical content across files costs one upload total.
3. **Manifest**: the new manifest is written, pointing at reused and new block objects alike.

A backup of a large, unchanged dataset therefore uploads only the manifest itself.

## Failed runs

If a run fails partway through, the snapshot is incomplete and must not be restored. backupd
deletes the source archives and the manifest it already uploaded for that snapshot; the partially
written snapshot never appears in `history` (state is recorded only on success).

Deduplication blocks are **not** deleted on failure - they are content-addressed, so they may be
referenced by other snapshots, and deleting them could corrupt good snapshots. Blocks orphaned by
the failed run are garbage-collected as soon as the run ends (and again during retention
pruning). A capture tool that exits nonzero (a failing database dump, a broken docker volume)
also fails the run: the partial stream is never stored as a snapshot.

## Retention cleanup

The retention pruner (keep-last / daily / weekly / monthly) deletes expired snapshots:

1. The snapshot's manifest and source archives.
2. Any block objects that are no longer referenced by *any* remaining manifest - this is what
   eventually reclaims storage from files that were deleted or changed in the source.

Blocks shared with surviving snapshots are kept. Garbage collection also runs after failed runs
and, on plans without a retention policy, after every successful run. `backupd prune <plan>`
applies the policy on demand, and `--dry-run` reports what would be deleted without deleting
anything.

## Encryption at rest

With `encryption.passphrase` set, every byte of content is encrypted before upload:

- **Key derivation**: the passphrase is stretched with **Argon2id** into a 256-bit key. The
  salt is generated on the first run and reused for the plan's lifetime (loaded from the most
  recent manifest), so every snapshot of a plan shares the same key - which keeps block
  deduplication working across runs while the passphrase is unchanged.
- **Blocks (files)**: each block is encrypted with **AES-256-GCM** using a *convergent* scheme -
  the key and nonce are derived (via HMAC) from the master key and the block's plaintext hash,
  and the plaintext hash is bound as the AEAD's additional authenticated data. Encryption is
  deterministic, so the same plaintext always produces the same ciphertext: deduplication still
  works on encrypted data. Each block decrypts and re-verifies its plaintext hash on restore and
  verify.
- **Archives (docker, kubernetes)**: streams are encrypted chunk-by-chunk with
  **AES-256-GCM** in 1 MiB frames (`BACKUPD-STREAM-ENC-1` header, random nonce, per-chunk
  counter), stored with a `.enc` suffix. Large dumps are never held in memory.
- **Object layout**: encrypted block objects are content-addressed by ciphertext hash, and the
  manifest records both the plaintext hash (for verification) and the object id.

Nothing stored is readable without the passphrase - see
[Encryption](encryption.md) for operational details.

## Memory use & large sources

backupd streams instead of buffering:

- File backup and restore move one 8 KiB block at a time; the whole file is never in memory (the
  second read of a changed file typically hits the page cache). Database dumps go through the same
  block pipeline.
- Container (docker, kubernetes) archives are uploaded as **streaming multipart** in 8 MiB parts -
  no temporary files, memory bounded by one part, and the upload is aborted cleanly on failure.
  Storages without multipart support fall back to spooling the stream to a temporary file so the
  upload size is known (a single S3 PUT is limited to 5 GiB).
- Restores and `verify` stream block-by-block and chunk-by-chunk, decrypting as they go.

## Rate limiting

A plan can set `rate-limit` (e.g. `10M`, `500K`) to throttle all transfers - uploads, restores,
and verification - with a token bucket that allows a one-second burst. See
[Configuration](configuration.md#plan).

## Dry runs

`backupd run <plan> --dry-run` executes the whole backup pipeline against a read-only storage
wrapper: sources are captured, files hashed, blocks computed - but nothing is uploaded, no hooks
run, no state is recorded, and no retention pruning happens. The reported size is exactly what a
real run would upload.

## What backupd does not do

- **No read-after-restore of system state**: restoring never modifies or deletes bucket objects.
- **No symlink following**: symlinks are stored as their target and recreated verbatim; nothing
  is ever written through a restored symlink, and restore paths are validated to stay inside the
  target directory.
