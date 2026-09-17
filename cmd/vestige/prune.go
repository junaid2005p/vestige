package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/junaid/vestige/internal/repository"
)

func runPrune(args []string) error {
	flags := flag.NewFlagSet("prune", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	last := flags.Int("keep-last", 0, "retain this many newest snapshots")
	daily := flags.Int("keep-daily", 0, "retain newest snapshot from this many days")
	weekly := flags.Int("keep-weekly", 0, "retain newest snapshot from this many ISO weeks")
	yes := flags.Bool("yes", false, "delete the snapshots selected by the policy")
	dry := flags.Bool("dry-run", false, "show snapshots selected by the policy")
	if err := flags.Parse(args); err != nil || len(flags.Args()) != 1 || *last < 0 || *daily < 0 || *weekly < 0 || (*yes && *dry) {
		return fmt.Errorf("usage: vestige prune [--keep-last N] [--keep-daily N] [--keep-weekly N] [--dry-run|--yes] <repo-dir>")
	}
	r, err := repository.Open(flags.Args()[0])
	if err != nil {
		return err
	}
	candidates, err := r.RetentionCandidates(repository.RetentionPolicy{KeepLast: *last, KeepDaily: *daily, KeepWeekly: *weekly})
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		fmt.Println("retention policy selects no snapshots")
		return nil
	}
	for _, candidate := range candidates {
		fmt.Println(candidate.SnapshotID)
	}
	if !*yes {
		fmt.Printf("%d snapshots selected; rerun with --yes to delete manifests\n", len(candidates))
		return nil
	}
	for _, candidate := range candidates {
		if _, err := r.DeleteSnapshot(candidate.SnapshotID, false, 0); err != nil {
			return err
		}
	}
	fmt.Printf("deleted %d snapshots; run gc --dry-run to inspect unreachable chunks\n", len(candidates))
	return nil
}
