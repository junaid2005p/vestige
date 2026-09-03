package main

import (
	"fmt"
	"sort"

	"github.com/junaid/vestige/internal/model"
	"github.com/junaid/vestige/internal/repository"
)

func runDiff(args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("usage: vestige diff <repo-dir> <older-snapshot-id> <newer-snapshot-id>")
	}
	r, err := repository.Open(args[0])
	if err != nil {
		return err
	}
	older, err := r.ReadManifest(args[1])
	if err != nil {
		return err
	}
	newer, err := r.ReadManifest(args[2])
	if err != nil {
		return err
	}
	oldEntries := entryMap(older.Files)
	newEntries := entryMap(newer.Files)
	paths := make([]string, 0, len(oldEntries)+len(newEntries))
	seen := make(map[string]bool)
	for p := range oldEntries {
		seen[p] = true
		paths = append(paths, p)
	}
	for p := range newEntries {
		if !seen[p] {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	changes := 0
	for _, p := range paths {
		old, hadOld := oldEntries[p]
		new, hadNew := newEntries[p]
		switch {
		case !hadOld:
			fmt.Printf("A\t%s\n", p)
			changes++
		case !hadNew:
			fmt.Printf("D\t%s\n", p)
			changes++
		case !sameEntry(old, new):
			fmt.Printf("M\t%s\n", p)
			changes++
		}
	}
	if changes == 0 {
		fmt.Println("no changes")
	}
	return nil
}

func entryMap(entries []model.FileEntry) map[string]model.FileEntry {
	out := make(map[string]model.FileEntry, len(entries))
	for _, entry := range entries {
		out[entry.Path] = entry
	}
	return out
}

func sameEntry(a, b model.FileEntry) bool {
	if a.Type != b.Type || a.Size != b.Size || a.Mode != b.Mode || len(a.Chunks) != len(b.Chunks) {
		return false
	}
	for i := range a.Chunks {
		if a.Chunks[i] != b.Chunks[i] {
			return false
		}
	}
	return true
}
