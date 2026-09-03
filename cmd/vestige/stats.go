package main

import (
	"fmt"

	"github.com/junaid/vestige/internal/repository"
)

func runStats(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: vestige stats <repo-dir>")
	}
	r, err := repository.Open(args[0])
	if err != nil {
		return err
	}
	chunks, physical, snapshots, logical, err := r.Stats()
	if err != nil {
		return err
	}
	saved := int64(0)
	if logical > physical {
		saved = logical - physical
	}
	percent := float64(0)
	if logical > 0 {
		percent = float64(saved) / float64(logical) * 100
	}
	fmt.Printf("snapshots: %d\nchunks: %d\nlogical snapshot bytes: %d\nphysical chunk bytes: %d\nspace saved: %d (%.2f%%)\n", snapshots, chunks, logical, physical, saved, percent)
	return nil
}
