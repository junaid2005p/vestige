# Vestige

Vestige is a local, content-defined, deduplicating backup engine written in Go. It stores each unique chunk once, publishes completed backups as immutable snapshots, and can restore or verify them later.

## Features

- Content-defined chunking with SHA-256 plaintext chunk IDs.
- Local repositories with optional `gzip` or `zstd` compression.
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
vestige verify <repo-dir> [snapshot-id|latest]
vestige delete [--dry-run|--yes] <repo-dir> <snapshot-id|latest>
vestige gc [--dry-run] <repo-dir>
```

`latest` may be used anywhere a snapshot ID is accepted; it resolves to the newest published snapshot. `restore` refuses an existing destination by default. Use `--overwrite` to replace only restored paths; `--clean-destination` additionally clears the destination and requires `--overwrite`. Restore validates every chunk before writing it.

`backup` accepts comma-separated, source-relative `path.Match` patterns for `--include` and `--exclude`; exclusions win. It can also record a message, tags, and immutable `key=value` labels with each snapshot.

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
