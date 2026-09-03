package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/junaid/vestige/internal/repository"
)

func runInit(args []string) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	compression := flags.String("compression", "none", "chunk compression: none, gzip, or zstd")
	if err := flags.Parse(args); err != nil || len(flags.Args()) != 1 || (*compression != "none" && *compression != "gzip" && *compression != "zstd") {
		return fmt.Errorf("usage: vestige init [--compression none|gzip|zstd] <repo-dir>")
	}
	r, err := repository.Init(flags.Args()[0], repository.RepositoryOptions{Compression: *compression})
	if err != nil {
		return err
	}
	fmt.Printf("repository ready: %s\ncompression: %s\n", r.Root, *compression)
	return nil
}
