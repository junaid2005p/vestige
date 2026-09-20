# Vestige

Vestige is a local, content-defined, deduplicating backup engine written in Go. It stores each unique chunk once, publishes completed backups as immutable snapshots, and can restore or verify them later.

## Features

- Content-defined chunking with SHA-256 plaintext chunk IDs.
- Local or S3-compatible repositories with optional `gzip` or `zstd` compression.
- Atomic snapshot publication, integrity verification, and bounded parallel chunk storage.
- Snapshot metadata, include/exclude filters, path search, and snapshot diffs.
- Safe restore defaults, snapshot deletion, stale-lock recovery, and garbage collection.
- A repeatable benchmark harness that measures backup, incremental reuse, verify, and restore.

## Quick start

Requires Go 1.25 or newer.

```powershell
go run ./cmd/vestige init --compression zstd .\example-repo
go run ./cmd/vestige backup --workers 4 --message "initial backup" . .\example-repo
go run ./cmd/vestige snapshots .\example-repo
go run ./cmd/vestige verify .\example-repo
go run ./cmd/vestige restore .\example-repo <snapshot-id> .\restored
```

Do not create the repository inside the directory being backed up; Vestige rejects that layout to avoid recursively backing up the repository itself.

## Encryption

Create an encrypted repository by providing a passphrase through the process
environment (it is never written to the repository) and passing `--encrypt`.
All later commands against that repository require the same environment value.

```powershell
$env:VESTIGE_PASSPHRASE = Read-Host "Vestige passphrase"
go run ./cmd/vestige init --encrypt .\encrypted-repo
go run ./cmd/vestige backup . .\encrypted-repo
```

Vestige derives an AES-256-GCM key using PBKDF2-HMAC-SHA-256 with a
repository-specific random salt, then uses it to wrap a random repository data
key. That data key encrypts chunk payloads and manifests; the repository
configuration retains only the salt, KDF parameters, wrapped data key, and a
non-secret encrypted key check. Losing the passphrase makes the backup
unrecoverable, so store it in a password manager or secret manager. Do not put
the passphrase directly on a command line.

To rotate a passphrase, set both environment variables and rewrap the data key.
Chunks and manifests are not rewritten.

```powershell
$env:VESTIGE_NEW_PASSPHRASE = Read-Host "New Vestige passphrase"
go run ./cmd/vestige key rotate .\encrypted-repo
```

## S3-compatible repositories

Use an S3 URL anywhere Vestige accepts a repository path. The bucket must
already exist; Vestige never creates buckets. The normal AWS SDK credential
chain is used, including environment variables, shared credentials files, IAM
roles, and AWS SSO profiles.

```powershell
$env:AWS_REGION = "ap-south-1"
go run ./cmd/vestige init --compression zstd s3://my-backups/vestige
go run ./cmd/vestige backup --encrypt C:\Users\me\Documents s3://my-backups/vestige
go run ./cmd/vestige restore s3://my-backups/vestige latest C:\restore
```

For MinIO and other compatible services, set `VESTIGE_S3_ENDPOINT` to the
service URL and `VESTIGE_S3_PATH_STYLE=true` when the service requires
path-style requests. Set `VESTIGE_S3_REGION` if the endpoint does not use the
region from your AWS configuration.

Vestige uploads chunks before it publishes a snapshot manifest. The manifest
is written with S3's conditional-create operation, so incomplete backups never
appear as snapshots. Writer locks and chunk uploads use the same conditional
write protection. Client-side encryption remains unchanged; S3 only receives
the encrypted chunk and manifest bytes.

The role or user needs these permissions for the repository prefix:
`s3:GetObject`, `s3:PutObject`, `s3:DeleteObject`, and `s3:ListBucket`.
Enable bucket versioning and default server-side encryption for production
repositories. Do not use lifecycle rules that expire current Vestige objects.

## Common commands

```text
vestige init [--compression none|gzip|zstd] <repo-dir>
vestige backup [--workers N] [--message TEXT] [--tags one,two] [--labels key=value] [--include glob] [--exclude glob] <source-dir> <repo-dir>
vestige snapshots <repo-dir>
vestige show <repo-dir> <snapshot-id|latest>
vestige diff <repo-dir> <older-snapshot-id|latest> <newer-snapshot-id|latest>
vestige find <repo-dir> <snapshot-id|latest> <glob>
vestige restore [--include glob] [--overwrite] [--clean-destination] <repo-dir> <snapshot-id|latest> <destination-dir>
vestige restore --stdout <repo-dir> <snapshot-id|latest> <source-relative-file>
vestige verify [--json] [--state checkpoint.json] <repo-dir> [snapshot-id|latest]
vestige check [--json] <repo-dir>
vestige key rotate <repo-dir>
vestige replicate <source-repo> <target-repo>
vestige recovery-kit export <repo-dir> <kit.zip>
vestige recovery-kit validate <kit.zip>
vestige drill [--snapshot snapshot-id|latest] [--json] <repo-dir> <empty-destination-dir>
vestige delete [--dry-run|--yes] <repo-dir> <snapshot-id|latest>
vestige gc [--dry-run] <repo-dir>
```

`latest` may be used anywhere a snapshot ID is accepted; it resolves to the newest published snapshot. `restore` refuses an existing destination by default. Use `--overwrite` to replace only restored paths; `--clean-destination` additionally clears the destination and requires `--overwrite`. Restore validates every chunk before writing it.

`backup` accepts comma-separated, source-relative `path.Match` patterns for `--include` and `--exclude`; exclusions win. It can also record a message, tags, and immutable `key=value` labels with each snapshot.

`check` is a read-only repository consistency scan. It detects invalid
manifests, missing or corrupt referenced chunks, and unreferenced chunks; use
`--json` for automation. Run it before `gc` when investigating a damaged
repository. The repository format and forward-migration policy are documented
in [FORMAT.md](FORMAT.md).

`verify` reads and hashes every unique referenced chunk. `--state` persists a
local checkpoint after each verified chunk, so rerunning the same command can
resume after an interruption; its checkpoint is removed after success.

## Recovery operations

`replicate` copies an encrypted repository to another encrypted repository,
decrypting only in memory and encrypting with the target's independent data
key. Set `VESTIGE_SOURCE_PASSPHRASE` and `VESTIGE_TARGET_PASSPHRASE` when the
repositories use different passphrases; both repositories must use the same
compression mode.

`recovery-kit export` writes encrypted configuration and manifests, but no
chunks or passphrase, to a new ZIP file. Validate it on another machine with
`VESTIGE_PASSPHRASE`. `drill` restores a selected snapshot into a new empty
destination and reports RPO and restore duration (RTO).

Read [SECURITY.md](SECURITY.md) before using a remote repository in production.

## Benchmarking

`bench` generates a deterministic mixed dataset, performs an initial and changed backup, verifies the repository, restores the latest snapshot, and compares the restored tree byte-for-byte. It writes JSON results and a companion CSV when `--output` is supplied.

```powershell
go run ./cmd/vestige bench --workdir D:\vestige-bench --output .\bench-results.json --profile-dir .\profiles
```

The default run uses a 128 MiB dataset and three trials. It refuses workloads above the explicit disk budget; increase `--max-disk-mib` deliberately for larger runs.

## Development

```powershell
go test ./...
go vet ./...
go build ./cmd/vestige
```

CI runs formatting checks, tests, vet, and builds on Windows and Linux.
