package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/junaid/vestige/internal/chunker"
	"github.com/junaid/vestige/internal/repository"
)

func TestCheckAndVerifyJSONCommands(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	repoPath := filepath.Join(root, "repo")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte("CLI reliability smoke test"), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := repository.Open(repoPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Backup(source, chunker.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	if err := runCheck([]string{"--json", repoPath}); err != nil {
		t.Fatalf("check command: %v", err)
	}
	state := filepath.Join(root, "verify-state.json")
	if err := runVerify([]string{"--json", "--state", state, repoPath}); err != nil {
		t.Fatalf("verify command: %v", err)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatal("successful CLI verification left a checkpoint behind")
	}
}
