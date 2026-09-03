package main

import (
	"fmt"

	"github.com/junaid/vestige/internal/repository"
)

func runVerify(args []string) error {
	if len(args) != 1 && len(args) != 2 {
		return fmt.Errorf("usage: vestige verify <repo-dir> [snapshot-id]")
	}
	r, err := repository.Open(args[0])
	if err != nil {
		return err
	}
	id := ""
	if len(args) == 2 {
		id = args[1]
	}
	if err := r.Verify(id); err != nil {
		return err
	}
	fmt.Println("verification successful")
	return nil
}
