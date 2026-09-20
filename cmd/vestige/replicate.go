package main

import (
	"fmt"
	"os"

	"github.com/junaid/vestige/internal/repository"
)

func runReplicate(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: vestige replicate <source-repo> <target-repo>")
	}
	sourcePassphrase := os.Getenv("VESTIGE_SOURCE_PASSPHRASE")
	if sourcePassphrase == "" {
		sourcePassphrase = os.Getenv("VESTIGE_PASSPHRASE")
	}
	targetPassphrase := os.Getenv("VESTIGE_TARGET_PASSPHRASE")
	if targetPassphrase == "" {
		targetPassphrase = os.Getenv("VESTIGE_PASSPHRASE")
	}
	source, err := repository.OpenWithOptions(args[0], repository.RepositoryOptions{Passphrase: sourcePassphrase})
	if err != nil {
		return err
	}
	target, err := repository.OpenWithOptions(args[1], repository.RepositoryOptions{Passphrase: targetPassphrase})
	if err != nil {
		return err
	}
	stats, err := source.ReplicateTo(target, 0)
	if err != nil {
		return err
	}
	fmt.Printf("snapshots copied: %d\nsnapshots already present: %d\nchunks copied: %d\nchunks reused: %d\n", stats.SnapshotsCopied, stats.SnapshotsSkipped, stats.ChunksCopied, stats.ChunksReused)
	return nil
}
