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

func TestSecurityRecoveryCommands(t *testing.T) {
	root := t.TempDir()
	sourceDir := filepath.Join(root, "source")
	sourceRepo := filepath.Join(root, "source-repo")
	targetRepo := filepath.Join(root, "target-repo")
	if err := os.Mkdir(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "file.txt"), []byte("security command test"), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := repository.Init(sourceRepo, repository.RepositoryOptions{Encrypt: true, Passphrase: "source passphrase"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := source.Backup(sourceDir, chunker.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Init(targetRepo, repository.RepositoryOptions{Encrypt: true, Passphrase: "target passphrase"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VESTIGE_SOURCE_PASSPHRASE", "source passphrase")
	t.Setenv("VESTIGE_TARGET_PASSPHRASE", "target passphrase")
	if err := runReplicate([]string{sourceRepo, targetRepo}); err != nil {
		t.Fatalf("replicate command: %v", err)
	}
	t.Setenv("VESTIGE_PASSPHRASE", "source passphrase")
	kit := filepath.Join(root, "kit.zip")
	if err := runRecoveryKit([]string{"export", sourceRepo, kit}); err != nil {
		t.Fatalf("recovery-kit export: %v", err)
	}
	if err := runRecoveryKit([]string{"validate", kit}); err != nil {
		t.Fatalf("recovery-kit validate: %v", err)
	}
	t.Setenv("VESTIGE_PASSPHRASE", "target passphrase")
	if err := runDrill([]string{"--json", targetRepo, filepath.Join(root, "drill")}); err != nil {
		t.Fatalf("drill command: %v", err)
	}
}
