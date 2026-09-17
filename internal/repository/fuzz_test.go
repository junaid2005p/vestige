package repository

import (
	"os"
	"path/filepath"
	"testing"
)

// ReadManifest is a trust boundary: arbitrary bytes from a repository must
// return a useful error, never panic or escape the snapshot directory.
func FuzzReadManifest(f *testing.F) {
	f.Add([]byte(`{"format_version":1,"snapshot_id":"fuzz","files":[]}`))
	f.Add([]byte("not json"))
	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := Open(filepath.Join(t.TempDir(), "repo"))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(r.snapshotsDir(), "fuzz")
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "manifest.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		_, _ = r.ReadManifest("fuzz")
	})
}
