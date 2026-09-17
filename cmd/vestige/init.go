package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/junaid/vestige/internal/repository"
)

func runInit(args []string) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	compression := flags.String("compression", "none", "chunk compression: none, gzip, or zstd")
	encrypt := flags.Bool("encrypt", false, "encrypt chunks and manifests using VESTIGE_PASSPHRASE")
	if err := flags.Parse(args); err != nil || len(flags.Args()) != 1 || (*compression != "none" && *compression != "gzip" && *compression != "zstd") {
		return fmt.Errorf("usage: vestige init [--compression none|gzip|zstd] [--encrypt] <repo-dir>")
	}
	r, err := repository.Init(flags.Args()[0], repository.RepositoryOptions{Compression: *compression, Encrypt: *encrypt, Passphrase: os.Getenv("VESTIGE_PASSPHRASE")})
	if err != nil {
		return err
	}
	fmt.Printf("repository ready: %s\ncompression: %s\nencryption: %t\n", r.Root, r.Compression(), r.Encrypted())
	return nil
}
