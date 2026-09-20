package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/junaid/vestige/internal/repository"
)

func runVerify(args []string) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	jsonOutput := flags.Bool("json", false, "emit a machine-readable report")
	statePath := flags.String("state", "", "checkpoint file used to resume an interrupted verification")
	if err := flags.Parse(args); err != nil || len(flags.Args()) != 1 && len(flags.Args()) != 2 {
		return fmt.Errorf("usage: vestige verify [--json] [--state checkpoint.json] <repo-dir> [snapshot-id|latest]")
	}
	r, err := repository.Open(flags.Args()[0])
	if err != nil {
		return err
	}
	id := ""
	if len(flags.Args()) == 2 {
		id = flags.Args()[1]
	}
	report, err := r.VerifyWithOptions(id, repository.VerifyOptions{StatePath: *statePath})
	if err != nil {
		return err
	}
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	fmt.Printf("verification successful\nsnapshots: %d\nchunks: %d\nverified chunks: %d\nresumed chunks: %d\n", report.Snapshots, report.Chunks, report.VerifiedChunks, report.ResumedChunks)
	return nil
}
