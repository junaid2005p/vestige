# Security model

## What encryption protects

Encrypted repositories use AES-256-GCM for stored chunk payloads and snapshot
manifests. A random repository data key performs that encryption. The
passphrase is processed with PBKDF2-HMAC-SHA-256 and wraps the data key; it is
never stored. `vestige key rotate` changes that wrapper only, so it does not
rewrite chunks or manifests. Repositories created before wrapped keys were
introduced are upgraded to this arrangement during their first rotation.

AES-GCM authenticates encrypted chunk and manifest bytes. Vestige also hashes
the restored plaintext against each chunk ID. A modified encrypted object, a
chunk placed under a different ID, or a malformed manifest causes verification
or restore to fail.

## What it does not hide

Object names, object sizes, object counts, upload times, snapshot IDs, and the
SHA-256 chunk IDs remain visible to whoever can list the repository. Chunk IDs
can reveal equality with content an attacker already possesses; they are not a
password hash. Source paths, file names, tags, labels, and file contents are
inside encrypted manifests and chunks.

Vestige does not prevent rollback by a storage operator that can replace the
whole repository with an earlier valid set of objects. Keep an independent
record of expected snapshot IDs when rollback detection matters.

## S3 assumptions

Use TLS, least-privilege credentials, and a bucket owned by an account you
trust. Vestige relies on S3 conditional writes for chunk, manifest, and lock
publication, but cannot protect against an account owner changing bucket
policies, deleting objects, or serving an older valid repository. Enable S3
versioning and provider-side encryption as separate defenses; neither replaces
client-side encryption.

## Deletion and versioned buckets

`vestige delete` removes a manifest and `vestige gc` removes unreachable current
objects. In a versioned bucket, those operations normally create delete markers
or noncurrent versions. Old encrypted data can remain recoverable to principals
with version access until bucket-version retention and lifecycle policy remove
it. Do not claim secure erasure from these commands.

## Recovery material

A recovery kit contains the encrypted config and manifests, not chunks or a
passphrase. Store it separately from the repository and protect it as sensitive
metadata. `vestige recovery-kit validate` checks its passphrase and encrypted
metadata on another machine; it does not prove that the off-site chunk set is
available. Use `vestige drill` against the actual replica for that evidence.
