package main

import (
	"fmt"

	"github.com/junaid/vestige/internal/repository"
)

func runSnapshots(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: vestige snapshots <repo-dir>")
	}
	r, err := repository.Open(args[0])
	if err != nil {
		return err
	}
	items, err := r.Snapshots()
	if err != nil {
		return err
	}
	for _, m := range items {
		var bytes int64
		var files int
		for _, e := range m.Files {
			if e.Type == "file" {
				files++
				bytes += e.Size
			}
		}
		fmt.Printf("%s\t%s\t%d files\t%d bytes\n", m.SnapshotID, m.CreatedAt, files, bytes)
	}
	return nil
}
