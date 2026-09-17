# Vestige benchmark report

**Date:** September 16, 2026
**Environment:** Windows 11, Ryzen 7 4800H, 16 GB RAM, Go 1.25.3

## Summary

Vestige was tested with a repeatable 1 GiB dataset containing 1,024 files.
Each result below is the median of three runs.

The clearest result is incremental backup efficiency: when 10% of files were
changed, Vestige reused **91.3% of existing chunks**. Gzip reduced stored chunk
data from **0.901 GiB to 0.701 GiB**, a **22.2% reduction**.

The benchmark command also verifies the backup, restores the latest snapshot,
and compares the restored files with the original files. Every recorded run
completed those checks.

## Test setup

| Item | Value |
| --- | --- |
| Dataset size | 1 GiB |
| Files | 1,024 files, 1 MiB each |
| File mix | 60% random-like data, 20% repeated data, 20% text |
| Incremental change | 10% of files replaced unless noted |
| Runs per setting | 3 |
| Reported value | Median |
| Storage | Local disk on the same Windows machine |

These are local-disk results. Windows file caching was not cleared between
runs, so the numbers should not be treated as cloud, network, or cold-disk
performance.

## Backup and restore performance

No compression; 10% of files changed.

| Workers | Initial backup | Incremental backup | Restore | Verify | Chunk reuse |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 55.47 MiB/s | 69.31 MiB/s | 106.80 MiB/s | 7.73 s | 91.28% |
| 2 | 60.33 MiB/s | 72.76 MiB/s | 111.31 MiB/s | 7.62 s | 91.28% |
| 4 | 50.79 MiB/s | 63.99 MiB/s | 93.90 MiB/s | 8.65 s | 91.28% |
| 8 | 59.27 MiB/s | 72.44 MiB/s | 107.78 MiB/s | 7.79 s | 91.28% |
| 16 | 60.17 MiB/s | 73.93 MiB/s | 123.29 MiB/s | 5.99 s | 91.28% |

More workers did not produce a consistent speed increase on this machine. The
best initial-backup result was 60.33 MiB/s with two workers; the fastest restore
was 123.29 MiB/s with 16 workers.

## Compression results

One worker; 10% of files changed.

| Compression | Initial backup | Incremental backup | Restore | Stored chunks | Space saved |
| --- | ---: | ---: | ---: | ---: | ---: |
| None | 55.47 MiB/s | 69.31 MiB/s | 106.80 MiB/s | 0.901 GiB | 54.93% |
| Gzip | 41.04 MiB/s | 87.53 MiB/s | 131.16 MiB/s | 0.701 GiB | 64.95% |
| Zstd | 81.44 MiB/s | 117.48 MiB/s | 203.21 MiB/s | 0.700 GiB | 64.99% |

Gzip used 22.2% less chunk storage than no compression, but its initial backup
was 26.0% slower. Zstd reached almost the same storage size in a later run; use
the storage result as the reliable comparison, not an exact speed comparison.

## Effect of changed files

One worker; no compression.

| Changed files | Reused chunks | Stored chunks | Space saved |
| ---: | ---: | ---: | ---: |
| 1% | 99.12% | 0.812 GiB | 59.42% |
| 5% | 95.77% | 0.852 GiB | 57.42% |
| 10% | 91.28% | 0.901 GiB | 54.93% |
| 20% | 81.24% | 1.001 GiB | 49.95% |

As expected, changing more files lowers reuse. Even after replacing one in five
files, Vestige reused 81.24% of chunks on this dataset.

## Reproducing the benchmark

```powershell
go run ./cmd/vestige bench --workdir D:\vestige-bench --dataset-mib 1024 --trials 3 --output .\bench-results.json
```

The raw JSON and CSV files in [`raw/`](raw/) contain the individual trial
results. CPU and memory profiles are available in [`profiles/`](profiles/).
