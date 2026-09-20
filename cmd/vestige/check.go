package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/junaid/vestige/internal/repository"
)

func runCheck(args []string) error {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	jsonOutput := flags.Bool("json", false, "emit a machine-readable report")
	if err := flags.Parse(args); err != nil || len(flags.Args()) != 1 {
		return fmt.Errorf("usage: vestige check [--json] <repo-dir>")
	}
	r, err := repository.Open(flags.Args()[0])
	if err != nil {
		return err
	}
	report, err := r.Check()
	if err != nil {
		return err
	}
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	fmt.Printf("format version: %d\nsnapshots: %d\nreferenced chunks: %d\nstored chunks: %d\norphan chunks: %d\n", report.FormatVersion, report.Snapshots, report.ReferencedChunks, report.StoredChunks, report.OrphanChunks)
	if !report.Healthy() {
		for _, issue := range report.Issues {
			fmt.Printf("%s: %s: %s\n", issue.Kind, issue.Object, issue.Detail)
		}
		return fmt.Errorf("repository check found %d issue(s)", len(report.Issues))
	}
	fmt.Println("repository check successful")
	return nil
}
