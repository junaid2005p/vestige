package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/junaid/vestige/internal/repository"
)

func runDrill(args []string) error {
	flags := flag.NewFlagSet("drill", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	snapshot := flags.String("snapshot", "latest", "snapshot ID to restore")
	jsonOutput := flags.Bool("json", false, "emit a machine-readable report")
	if err := flags.Parse(args); err != nil || len(flags.Args()) != 2 {
		return fmt.Errorf("usage: vestige drill [--snapshot snapshot-id|latest] [--json] <repo-dir> <empty-destination-dir>")
	}
	r, err := repository.Open(flags.Args()[0])
	if err != nil {
		return err
	}
	report, err := r.DisasterRecoveryDrill(*snapshot, flags.Args()[1])
	if err != nil {
		return err
	}
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	fmt.Printf("disaster recovery drill successful\nsnapshot: %s\nfiles restored: %d\nRPO seconds: %.3f\nRTO seconds: %.3f\n", report.SnapshotID, report.Files, report.RPOSeconds, report.RTOSeconds)
	return nil
}
