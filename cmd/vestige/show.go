package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/junaid/vestige/internal/repository"
)

func runShow(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: vestige show <repo-dir> <snapshot-id>")
	}
	r, err := repository.Open(args[0])
	if err != nil {
		return err
	}
	m, err := r.ReadManifest(args[1])
	if err != nil {
		return err
	}
	var files int
	var logical int64
	for _, entry := range m.Files {
		if entry.Type == "file" {
			files++
			logical += entry.Size
		}
	}
	var labels []string
	for key, value := range m.Labels {
		labels = append(labels, key+"="+value)
	}
	sort.Strings(labels)
	fmt.Printf("snapshot: %s\ncreated: %s\nsource: %s\ntags: %s\nlabels: %s\ndescription: %s\nfiles: %d\nlogical bytes: %d\nchunker: %s (min=%d target=%d max=%d)\n", m.SnapshotID, m.CreatedAt, m.Source, strings.Join(m.Tags, ", "), strings.Join(labels, ", "), m.Description, files, logical, m.Chunking.Algorithm, m.Chunking.MinSize, m.Chunking.TargetSize, m.Chunking.MaxSize)
	return nil
}
