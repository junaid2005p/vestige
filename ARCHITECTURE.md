# Vestige Architecture

```text
source tree → traversal → content-defined chunker → SHA-256 → chunk store
                    │                                      │
                    └──────── manifest entries ◀───────────┘

completed manifest → snapshot list / verify / restore → destination tree
```

Chunks are addressed by the SHA-256 hash of plaintext content. The repository stores each ID once; optional gzip changes only stored representation, not chunk identity. A snapshot manifest is staged and published only after its chunks are stored. Restore validates chunk hashes before writing files, and manifest paths are validated before joining them to a destination.

## Restore destination safety

Restore creates a new destination by default, avoiding accidental overwrites. `--overwrite` is an explicit opt-in that replaces only restored file paths. `--clean-destination` is stronger: it requires `--overwrite`, empties an existing destination before writing, and refuses filesystem roots plus paths that overlap either the repository or the snapshot's recorded source. Cleaning is necessarily a destructive operation; a filesystem failure while it is running can leave the destination partially cleaned.

For extraction pipelines, `restore --stdout` finds one exact source-relative regular file and streams verified plaintext chunks to standard output. It does not create or modify a destination tree.

## Snapshot metadata

Snapshots retain their source path, creation time, optional description, tags, and immutable string labels. Tags are lightweight categorization; labels are unique `key=value` metadata intended for exact filtering by tools. Labels are part of the published manifest and are validated on both publication and read, so they never require a mutable side database.
