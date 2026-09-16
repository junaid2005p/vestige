package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		usage()
		return nil
	}
	switch args[0] {
	case "init":
		return runInit(args[1:])
	case "backup":
		return runBackup(args[1:])
	case "restore":
		return runRestore(args[1:])
	case "snapshots":
		return runSnapshots(args[1:])
	case "show":
		return runShow(args[1:])
	case "find":
		return runFind(args[1:])
	case "diff":
		return runDiff(args[1:])
	case "stats":
		return runStats(args[1:])
	case "verify":
		return runVerify(args[1:])
	case "gc":
		return runGC(args[1:])
	case "recover":
		return runRecover(args[1:])
	case "delete":
		return runDelete(args[1:])
	case "bench":
		return runBench(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage() {
	fmt.Print(`Vestige is a local, deduplicating backup engine.

Usage:
  vestige init [--compression none|gzip|zstd] <repo-dir>
  vestige backup [--workers N] [--compression none|gzip|zstd] [--message TEXT] [--tags one,two] [--labels key=value] [--include glob] [--exclude glob] [--stale-lock-after DURATION] <source-dir> <repo-dir>
  vestige restore [--include glob] [--overwrite] [--clean-destination] <repo-dir> <snapshot-id|latest> <destination-dir>
  vestige restore --stdout <repo-dir> <snapshot-id|latest> <source-relative-file>
  vestige snapshots <repo-dir>
  vestige show <repo-dir> <snapshot-id|latest>
  vestige find <repo-dir> <snapshot-id|latest> <glob>
  vestige find --chunk <sha256> <repo-dir> <snapshot-id|latest>
  vestige diff <repo-dir> <older-snapshot-id|latest> <newer-snapshot-id|latest>
  vestige stats <repo-dir>
  vestige verify <repo-dir> [snapshot-id|latest]
  vestige gc [--dry-run] [--stale-lock-after DURATION] <repo-dir>
  vestige recover --stale-lock-after DURATION <repo-dir>
  vestige delete [--dry-run|--yes] [--stale-lock-after DURATION] <repo-dir> <snapshot-id|latest>
  vestige bench --workdir <parent-dir> [--dataset-mib 128] [--trials 3] [--compression none|gzip|zstd] [--profile-dir profiles] [--output results.json]
`)
}
