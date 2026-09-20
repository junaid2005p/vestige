package main

import (
	"fmt"
	"os"

	"github.com/junaid/vestige/internal/repository"
)

func runKey(args []string) error {
	if len(args) != 2 || args[0] != "rotate" {
		return fmt.Errorf("usage: vestige key rotate <repo-dir>")
	}
	newPassphrase := os.Getenv("VESTIGE_NEW_PASSPHRASE")
	if newPassphrase == "" {
		return fmt.Errorf("set VESTIGE_NEW_PASSPHRASE to the replacement passphrase")
	}
	r, err := repository.Open(args[1])
	if err != nil {
		return err
	}
	if err := r.RotatePassphrase(newPassphrase, 0); err != nil {
		return err
	}
	fmt.Println("repository passphrase rotated; chunks and manifests were not rewritten")
	return nil
}
