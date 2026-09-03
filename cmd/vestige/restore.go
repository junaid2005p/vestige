package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/junaid/vestige/internal/repository"
)

func runRestore(args []string) error {
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	include := flags.String("include", "", "comma-separated source-relative glob patterns")
	overwrite := flags.Bool("overwrite", false, "replace matching files in an existing destination")
	cleanDestination := flags.Bool("clean-destination", false, "remove existing destination contents before restore (requires --overwrite)")
	stdout := flags.Bool("stdout", false, "write one source-relative file to standard output")
	if err := flags.Parse(args); err != nil || len(flags.Args()) != 3 {
		return fmt.Errorf("usage: vestige restore [--include glob] [--overwrite] [--clean-destination] <repo-dir> <snapshot-id> <destination-dir>\n       vestige restore --stdout <repo-dir> <snapshot-id> <source-relative-file>")
	}
	paths := flags.Args()
	r, err := repository.Open(paths[0])
	if err != nil {
		return err
	}
	if *stdout {
		if *include != "" || *overwrite || *cleanDestination {
			return fmt.Errorf("--stdout cannot be combined with --include, --overwrite, or --clean-destination")
		}
		return r.RestoreFile(paths[1], paths[2], os.Stdout)
	}
	if err := r.RestoreWithOptions(paths[1], paths[2], repository.RestoreOptions{
		Includes: splitPatterns(*include), Overwrite: *overwrite, CleanDestination: *cleanDestination,
	}); err != nil {
		return err
	}
	fmt.Println("restore completed")
	return nil
}

func splitPatterns(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, pattern := range parts {
		if pattern = strings.TrimSpace(pattern); pattern != "" {
			out = append(out, pattern)
		}
	}
	return out
}
