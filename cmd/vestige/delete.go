package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/junaid/vestige/internal/repository"
)

func runDelete(args []string) error {
	flags := flag.NewFlagSet("delete", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	yes := flags.Bool("yes", false, "delete the snapshot after showing its scope")
	dryRun := flags.Bool("dry-run", false, "show deletion scope without changing the repository")
	staleLockAfter := flags.Duration("stale-lock-after", 0, "explicitly recover a writer lock older than this duration")
	if err := flags.Parse(args); err != nil || len(flags.Args()) != 2 || *staleLockAfter < 0 || (*yes && *dryRun) {
		return fmt.Errorf("usage: vestige delete [--dry-run|--yes] [--stale-lock-after DURATION] <repo-dir> <snapshot-id>")
	}
	r, err := repository.Open(flags.Args()[0])
	if err != nil { return err }
	stats, err := r.DeleteSnapshot(flags.Args()[1], !*yes, *staleLockAfter)
	if err != nil { return err }
	if stats.DryRun {
		fmt.Printf("preview: snapshot %s\nfiles: %d\nlogical bytes: %d\nreferenced chunks: %d\nrun again with --yes to delete the manifest; then run gc --dry-run to see reclaimable chunks\n", stats.SnapshotID, stats.Files, stats.LogicalBytes, stats.ReferencedChunks)
		return nil
	}
	fmt.Printf("deleted snapshot: %s\nfiles: %d\nlogical bytes removed from snapshot history: %d\nreferenced chunks pending gc: %d\n", stats.SnapshotID, stats.Files, stats.LogicalBytes, stats.ReferencedChunks)
	return nil
}
