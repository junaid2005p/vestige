package main

import (
	"fmt"
	"os"

	"github.com/junaid/vestige/internal/repository"
)

func runRecoveryKit(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: vestige recovery-kit export <repo-dir> <kit.zip>\n       vestige recovery-kit validate <kit.zip>")
	}
	switch args[0] {
	case "export":
		if len(args) != 3 {
			return fmt.Errorf("usage: vestige recovery-kit export <repo-dir> <kit.zip>")
		}
		r, err := repository.Open(args[1])
		if err != nil {
			return err
		}
		report, err := r.ExportRecoveryKit(args[2])
		if err != nil {
			return err
		}
		fmt.Printf("recovery kit written\nrepository format: %d\nsnapshots: %d\n", report.RepositoryFormat, report.Snapshots)
		return nil
	case "validate":
		if len(args) != 2 {
			return fmt.Errorf("usage: vestige recovery-kit validate <kit.zip>")
		}
		report, err := repository.ValidateRecoveryKit(args[1], os.Getenv("VESTIGE_PASSPHRASE"))
		if err != nil {
			return err
		}
		fmt.Printf("recovery kit is valid\nrepository format: %d\nsnapshots: %d\n", report.RepositoryFormat, report.Snapshots)
		return nil
	default:
		return fmt.Errorf("usage: vestige recovery-kit export <repo-dir> <kit.zip>\n       vestige recovery-kit validate <kit.zip>")
	}
}
