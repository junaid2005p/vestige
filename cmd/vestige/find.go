package main

import (
	"flag"
	"fmt"
	"io"
	"path"

	"github.com/junaid/vestige/internal/repository"
)

// runFind lists snapshot entries whose slash-separated path matches glob.
func runFind(args []string) error {
	flags := flag.NewFlagSet("find", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	chunkID := flags.String("chunk", "", "find files referencing this SHA-256 chunk ID")
	if err := flags.Parse(args); err != nil || ((*chunkID == "" && len(flags.Args()) != 3) || (*chunkID != "" && len(flags.Args()) != 2)) {
		return fmt.Errorf("usage: vestige find <repo-dir> <snapshot-id> <glob>\n       vestige find --chunk <sha256> <repo-dir> <snapshot-id>")
	}
	paths := flags.Args()
	r, err := repository.Open(paths[0])
	if err != nil {
		return err
	}
	if *chunkID != "" {
		files, err := r.FilesReferencingChunk(paths[1], *chunkID)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return fmt.Errorf("no files reference chunk %q", *chunkID)
		}
		for _, entry := range files {
			fmt.Printf("file\t%d\t%s\n", entry.Size, entry.Path)
		}
		return nil
	}
	m, err := r.ReadManifest(paths[1])
	if err != nil {
		return err
	}
	matched := 0
	for _, entry := range m.Files {
		ok, err := path.Match(paths[2], entry.Path)
		if err != nil {
			return fmt.Errorf("invalid glob: %w", err)
		}
		if !ok {
			continue
		}
		matched++
		if entry.Type == "file" {
			fmt.Printf("file\t%d\t%s\n", entry.Size, entry.Path)
		} else {
			fmt.Printf("directory\t-\t%s\n", entry.Path)
		}
	}
	if matched == 0 {
		return fmt.Errorf("no paths matched %q", paths[2])
	}
	return nil
}
