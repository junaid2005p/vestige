package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/junaid/vestige/internal/repository"
)

func runGC(args []string) error {
	flags := flag.NewFlagSet("gc", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dryRun := flags.Bool("dry-run", false, "report unreachable chunks without deleting them")
	staleLockAfter := flags.Duration("stale-lock-after", 0, "explicitly recover a writer lock older than this duration")
	if err := flags.Parse(args); err != nil || len(flags.Args()) != 1 || *staleLockAfter < 0 {
		return fmt.Errorf("usage: vestige gc [--dry-run] [--stale-lock-after DURATION] <repo-dir>")
	}
	r, err := repository.Open(flags.Args()[0])
	if err != nil {
		return err
	}
	stats, err := r.GarbageCollect(*dryRun, *staleLockAfter)
	if err != nil {
		return err
	}
	mode := "reclaimed"
	if stats.DryRun {
		mode = "reclaimable"
	}
	fmt.Printf("reachable chunks: %d\norphan chunks: %d\n%s bytes: %d\n", stats.ReachableChunks, stats.OrphanChunks, mode, stats.ReclaimedBytes)
	return nil
}

func runRecover(args []string) error {
	flags := flag.NewFlagSet("recover", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	staleLockAfter := flags.Duration("stale-lock-after", 0, "recover a writer lock older than this duration")
	if err := flags.Parse(args); err != nil || len(flags.Args()) != 1 || *staleLockAfter <= 0 {
		return fmt.Errorf("usage: vestige recover --stale-lock-after DURATION <repo-dir>")
	}
	r, err := repository.Open(flags.Args()[0])
	if err != nil {
		return err
	}
	removed, err := r.Recover(*staleLockAfter)
	if err != nil {
		return err
	}
	fmt.Printf("removed abandoned staging directories: %d\n", removed)
	return nil
}
