package repository

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/junaid/vestige/internal/model"
)

const recoveryKitVersion = "vestige-recovery-kit/v1"

type recoveryKitInfo struct {
	Version          string `json:"version"`
	RepositoryFormat int    `json:"repository_format"`
}

// RecoveryKitReport describes metadata exported to, or validated from, a kit.
type RecoveryKitReport struct {
	RepositoryFormat int `json:"repository_format"`
	Snapshots        int `json:"snapshots"`
}

// ExportRecoveryKit writes the encrypted configuration and encrypted snapshot
// manifests to a new ZIP file. It deliberately excludes chunk data.
func (r *Repository) ExportRecoveryKit(destination string) (RecoveryKitReport, error) {
	var report RecoveryKitReport
	if !r.Encrypted() {
		return report, errors.New("recovery kits require an encrypted repository")
	}
	f, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return report, fmt.Errorf("create recovery kit: %w", err)
	}
	success := false
	defer func() {
		f.Close()
		if !success {
			os.Remove(destination)
		}
	}()
	w := zip.NewWriter(f)
	write := func(name string, data []byte) error {
		entry, err := w.Create(name)
		if err != nil {
			return err
		}
		_, err = entry.Write(data)
		return err
	}
	info, err := marshalJSON(recoveryKitInfo{Version: recoveryKitVersion, RepositoryFormat: r.config.FormatVersion})
	if err != nil {
		return report, err
	}
	if err := write("recovery-kit.json", info); err != nil {
		return report, err
	}
	config, _, err := r.store.Get("config.json")
	if err != nil {
		return report, fmt.Errorf("read repository config: %w", err)
	}
	if err := write("config.json", config); err != nil {
		return report, err
	}
	manifests, err := r.Snapshots()
	if err != nil {
		return report, err
	}
	for _, manifest := range manifests {
		key := r.manifestKey(manifest.SnapshotID)
		data, _, err := r.store.Get(key)
		if err != nil {
			return report, fmt.Errorf("read %s: %w", key, err)
		}
		if err := write(key, data); err != nil {
			return report, err
		}
		report.Snapshots++
	}
	if err := w.Close(); err != nil {
		return report, err
	}
	if err := f.Close(); err != nil {
		return report, err
	}
	success = true
	report.RepositoryFormat = r.config.FormatVersion
	return report, nil
}

// ValidateRecoveryKit verifies a kit from a clean machine without requiring a
// repository directory or chunk objects. It validates config, passphrase, and
// authenticated manifest contents.
func ValidateRecoveryKit(source, passphrase string) (RecoveryKitReport, error) {
	var report RecoveryKitReport
	z, err := zip.OpenReader(source)
	if err != nil {
		return report, fmt.Errorf("open recovery kit: %w", err)
	}
	defer z.Close()
	files := map[string]*zip.File{}
	for _, file := range z.File {
		if _, exists := files[file.Name]; exists {
			return report, fmt.Errorf("duplicate recovery-kit entry %q", file.Name)
		}
		files[file.Name] = file
	}
	read := func(name string) ([]byte, error) {
		file := files[name]
		if file == nil {
			return nil, fmt.Errorf("recovery kit is missing %s", name)
		}
		rd, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer rd.Close()
		return io.ReadAll(rd)
	}
	infoData, err := read("recovery-kit.json")
	if err != nil {
		return report, err
	}
	var info recoveryKitInfo
	if err := json.Unmarshal(infoData, &info); err != nil || info.Version != recoveryKitVersion {
		return report, errors.New("invalid recovery kit metadata")
	}
	configData, err := read("config.json")
	if err != nil {
		return report, err
	}
	var config model.RepositoryConfig
	if err := json.Unmarshal(configData, &config); err != nil {
		return report, fmt.Errorf("invalid recovery-kit config: %w", err)
	}
	if config.FormatVersion != model.FormatVersion || info.RepositoryFormat != config.FormatVersion || config.Encryption == nil {
		return report, errors.New("unsupported recovery-kit repository format")
	}
	probe := &Repository{config: config}
	if err := probe.openEncryption(passphrase); err != nil {
		return report, err
	}
	for name, file := range files {
		if !strings.HasPrefix(name, "snapshots/") || !strings.HasSuffix(name, "/manifest.enc") || strings.Count(name, "/") != 2 {
			continue
		}
		id := strings.Split(name, "/")[1]
		rd, err := file.Open()
		if err != nil {
			return report, err
		}
		data, readErr := io.ReadAll(rd)
		closeErr := rd.Close()
		if readErr != nil {
			return report, readErr
		}
		if closeErr != nil {
			return report, closeErr
		}
		plain, err := probe.decrypt(data, []byte("vestige/manifest/"+id))
		if err != nil {
			return report, fmt.Errorf("decrypt snapshot %s: %w", id, err)
		}
		var manifest model.Manifest
		if err := json.Unmarshal(plain, &manifest); err != nil {
			return report, fmt.Errorf("invalid snapshot %s: %w", id, err)
		}
		if err := validateManifest(manifest); err != nil || manifest.SnapshotID != id {
			return report, fmt.Errorf("invalid snapshot %s", id)
		}
		report.Snapshots++
	}
	report.RepositoryFormat = config.FormatVersion
	return report, nil
}
