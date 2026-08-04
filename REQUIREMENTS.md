# Vestige — Incremental Roadmap

Vestige should evolve from a correct local deduplicating backup engine into a reliable, measurable systems project. Each increment must preserve restore correctness, add automated tests, and include a short design note whenever it changes the repository format, an invariant, or benchmarked behavior. The aim is a strong resume project now and a credible research project later.

## Current baseline

The current implementation has local content-addressed storage, content-defined chunking, SHA-256 plaintext chunk IDs, JSON snapshot manifests, atomic snapshot publication, restore, integrity verification, duplicate reuse, bounded parallel chunk storage, and optional gzip compression. Treat the serial/no-compression configuration as the baseline for all later claims.

## Incremental improvements

### 1. Presentation-ready MVP

- Add `vestige init <repo-dir>` for explicit repository creation.
- Add `vestige show <repo-dir> <snapshot-id>` for snapshot metadata and configuration.
- Add `--json` to every command for scripts and benchmark automation.
- Define stable error categories and exit codes.
- Improve readable byte sizes and durations while retaining raw values in JSON.
- Add PowerShell, Bash, and Zsh shell completion.
- Add a one-command demo: backup, modify, snapshot, verify, restore, compare.
- Add an architecture diagram and a repository-format document.
- Add changelog, semantic versioning, release notes, and reproducible build instructions.
- Add CI for format, test, vet, build, and end-to-end tests on Windows and Linux.

### 2. Snapshot usability

- Add labels, tags, descriptions, and source metadata to snapshots.
- Add `diff` to compare paths, metadata, and content between snapshots.
- Add `find` for snapshot path search and hash lookup.
- Add include/exclude rules with documented glob behavior.
- Add a dry-run mode that writes nothing.
- Add selective restore by relative path or glob.
- Add single-file restore to standard output.
- Add safe `--overwrite` and `--clean-destination` restore modes after explicit safeguards.
- Add terminal progress indicators that never corrupt JSON output.
- Add cancellation handling for Ctrl+C and an explicit interrupted-operation result.
- Add a documented partial-snapshot mode; default behavior remains fail-on-unreadable-file.

### 3. Reliability and recovery

- Add a repository write lock and document concurrent-reader behavior.
- Add safe stale-lock recovery after crashes.
- Add durability modes with a safe default and an explicit benchmark-only fast mode.
- Add a journal for multi-step mutations.
- Detect and recover or clean abandoned staging directories.
- Add resumable backups that reuse verified completed work.
- Add fault-injection tests for write, sync, rename, publication, and restore failures.
- Add detailed repository-health reports for invalid manifests, missing chunks, corruption, and orphaned chunks.
- Add format migration and repair tooling before incompatible layout changes.
- Add periodic verification sampling and a full-audit mode.

### 4. Snapshot lifecycle

- Add deletion with confirmation and dry-run preview.
- Add mark-and-sweep garbage collection for unreachable chunks.
- Add `gc --dry-run` reporting reclaimable chunks and bytes.
- Add retention policies: keep-last, hourly/daily/weekly/monthly, age-based, tags, and pins.
- Add a retention simulation command.
- Add repository quota warnings and an optional hard size limit.
- Add safe cleanup of temporary data after crash recovery is trustworthy.
- Add pack/compact support only after loose-file overhead is quantified.

### 5. Metadata scaling

- Measure directory lookup before replacing it.
- Add a persistent index for chunk location, stored size, references, and verification state.
- Compare an embedded index backend against a simple sorted-file baseline.
- Make the index rebuildable from manifests and chunks.
- Coordinate transactional index updates with snapshot publication.
- Add index consistency checks, repair, compaction, and rebuild commands.
- Add bloom filters only if negative lookup profiling justifies them.
- Add compact/streaming manifest formats only after JSON overhead is measured.
- Test 1M, 10M, and 50M chunk workloads where feasible.

### 6. Performance engineering

- Add counters for read, chunk, hash, lookup, compression, write, reuse, and blocked-I/O time.
- Publish counters through JSON and benchmark result files.
- Capture CPU, memory, block, mutex, and I/O profiles.
- Add independently configurable pipeline stages for traversal, reading, chunking, hashing, compression, and writing.
- Measure queue-size and backpressure effects instead of guessing.
- Add adaptive worker limits based on storage throughput and CPU saturation.
- Add file-level parallelism with deterministic manifests and bounded memory.
- Add low-priority rate limiting for background backup jobs.
- Add metadata-based incremental shortcuts with a safe full-read mode.
- Evaluate faster hashes only with a retained cryptographic integrity design.
- Add controlled performance-regression checks where CI hardware permits.

### 7. Storage efficiency

- Benchmark no compression versus gzip first.
- Zstd is implemented with `klauspost/compress`, a self-identifying chunk envelope, and raw fallback when compression would expand a chunk. Benchmark different zstd levels before exposing level tuning.
- Store algorithm and level in repository metadata.
- Skip compression when it expands data or saves too little to justify CPU cost.
- Evaluate entropy sampling only if it reduces meaningful wasted work.
- Compare fixed-size and content-defined chunking with identical datasets and storage rules.
- Make min/target/max chunk sizes configurable and record exact values in manifests.
- Add a chunk-size tuning runner for 256 KiB through 8 MiB target sizes.
- Test source code, documents, VM images, database-like files, archives, media, and random data.
- Add sparse-file detection and restore where platform support permits.
- Consider packed chunk segments after measuring small-file and directory overhead.

### 8. Security and filesystem fidelity

- Write a threat model before adding encryption or remote storage.
- Add authenticated encryption for chunks, manifests, and indexes.
- Document deduplication and metadata leakage tradeoffs.
- Design key handling, rotation, recovery assumptions, and secure defaults.
- Add repository configuration authentication and tamper detection.
- Harden log output and redact sensitive paths where appropriate.
- Add symlink support with no-follow backup behavior and safe restore.
- Add hard-link preservation, ownership, ACLs, timestamps, extended attributes, and sparse files behind tested platform capabilities.
- Document separate Windows, Linux, and macOS fidelity guarantees.
- Test long paths, case conflicts, reserved names, Unicode, malformed manifests, and hostile compressed input.

### 9. Remote and distributed capability

- Introduce a storage abstraction while retaining local disk as the reference backend.
- Add read-only repository export before remote writes.
- Add local-to-local replication with verification and resume support.
- Add HTTP or object-store backends with authentication, retries, rate limits, checksums, and resumable transfers.
- Add multi-destination backup with documented all-required and best-effort semantics.
- Add remote locks, leases, conflict handling, and eventual-consistency rules before concurrent remote writers.
- Publish clean-machine disaster-recovery instructions and test them.

### 10. Portfolio polish

- Publish tagged releases and downloadable binaries.
- Record a concise terminal demo showing deduplication, verification, corruption detection, and restore.
- Publish design docs explaining invariants, failure handling, and tradeoffs.
- Publish benchmark charts, raw results, scripts, machine details, methodology, and limitations.
- Add a comparison page against a plain copy and clearly scope differences from mature tools such as Borg or Restic.
- Write resume bullets supported by measured claims.
- Maintain a decision log for chunking, compression, indexing, storage layout, parallelism, and durability choices.

## Benchmarking

Benchmarking is a deliverable, not a final polish step. Keep a reproducible dataset generator with fixed seeds, a small smoke profile for local development/CI, and larger portfolio profiles. For every run record application commit, Go version, command line, chunking settings, compression mode, workers, CPU, RAM, storage device, filesystem, OS, cache state, and trial count. Run at least three trials and report medians plus spread. Measure logical bytes, complete repository bytes, bytes newly written, deduplication ratio, backup/restore/verify throughput, CPU time, peak memory, chunk counts, reuse, compression ratio, lookup latency, index size, and GC time. Required studies should include duplicate data, incremental changes of 1/5/10/20%, middle insertions for fixed versus content-defined chunking, scaling at 1/2/4/8/16 workers, compression modes/levels, chunk-size tuning, index scaling, and garbage collection. Preserve raw CSV or JSON with scripts and charts; state cold versus warm cache and limitations such as synthetic-data bias or storage bottlenecks.

The current `vestige bench` harness is the bounded MVP for this work: it uses deterministic mixed data, runs first and 10%-changed incremental backups, verify, restore, and byte comparison, and emits per-trial JSON/CSV plus min/median/max summaries. It records Go/runtime and benchmark configuration automatically. Host RAM, storage/filesystem, cache state, commit, and hardware must still be recorded with each published study. Its explicit disk budget is an estimate, not a substitute for checking available free space before larger studies.

## Possible novelty additions

Novelty begins only after the product and benchmark harness are reliable. Select one falsifiable hypothesis, define a baseline and candidate before coding, publish parameters and raw results, and report negative results honestly. The strongest initial option is adaptive content-defined chunking: estimate workload characteristics such as repetitiveness, entropy, or edit history; choose a chunking distribution; and test whether it improves storage reduction without an unacceptable throughput loss against one fixed policy. Other worthwhile directions are workload-aware chunking/compression for source trees, VM images, databases, documents, and media; a rule-based or learned pipeline controller that tunes worker counts and queues from observed backpressure; chunk placement optimized for restore locality; index/GC structures designed for tens of millions of chunks; change-pattern-aware chunking that preserves reuse under inserts; and policies that trade bounded extra storage for lower restore latency. Every experiment should include representative and adversarial datasets, a clear limitation section, and a conclusion about when the approach does not help.
