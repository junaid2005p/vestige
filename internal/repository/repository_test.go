package repository

import (
	"bytes"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/junaid/vestige/internal/chunker"
	"github.com/junaid/vestige/internal/model"
)

func TestBackupRestoreDeduplicatesAndVerifies(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	repoPath := filepath.Join(root, "repo")
	destination := filepath.Join(root, "restored")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	content := []byte("same file content\n")
	if err := os.WriteFile(filepath.Join(source, "a.txt"), content, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "b.txt"), content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "empty"), nil, 0644); err != nil {
		t.Fatal(err)
	}

	r, err := Open(repoPath)
	if err != nil {
		t.Fatal(err)
	}
	first, firstStats, err := r.Backup(source, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	if firstStats.ChunksCreated != 1 || firstStats.BytesWritten != int64(len(content)) {
		t.Fatalf("unexpected first stats: %+v", firstStats)
	}
	_, secondStats, err := r.Backup(source, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	if secondStats.ChunksCreated != 0 || secondStats.BytesWritten != 0 || secondStats.ChunksReused != 2 {
		t.Fatalf("expected full reuse, got %+v", secondStats)
	}
	if err := r.Verify(""); err != nil {
		t.Fatal(err)
	}
	if err := r.Restore(first.SnapshotID, destination); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"a.txt", "nested/b.txt", "empty"} {
		want, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(destination, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("restored %s differs", rel)
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(destination, "a.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0640 {
			t.Fatalf("restored permissions = %o, want 640", info.Mode().Perm())
		}
	}
}

func TestLatestSnapshotSelector(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("first"), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := Open(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := r.Backup(source, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("second"), 0644); err != nil {
		t.Fatal(err)
	}
	second, _, err := r.Backup(source, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	if first.SnapshotID == second.SnapshotID {
		t.Fatal("snapshot IDs must differ")
	}
	latest, err := r.ReadManifest("latest")
	if err != nil {
		t.Fatal(err)
	}
	if latest.SnapshotID != second.SnapshotID {
		t.Fatalf("latest = %s, want %s", latest.SnapshotID, second.SnapshotID)
	}
}

func TestLatestSnapshotRejectsEmptyRepository(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadManifest("latest"); err == nil {
		t.Fatal("latest selector succeeded for empty repository")
	}
}

func TestDeleteLatestSnapshotSelector(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := Open(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := r.Backup(source, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	stats, err := r.DeleteSnapshot("latest", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if stats.SnapshotID != snapshot.SnapshotID {
		t.Fatalf("deleted %s, want %s", stats.SnapshotID, snapshot.SnapshotID)
	}
	if _, err := r.ReadManifest(snapshot.SnapshotID); err == nil {
		t.Fatal("deleted latest snapshot is still readable")
	}
}

func TestVerifyDetectsCorruptedChunk(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	data := []byte("integrity matters")
	if err := os.WriteFile(filepath.Join(source, "a.txt"), data, 0644); err != nil {
		t.Fatal(err)
	}
	r, err := Open(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Backup(source, smallConfig()); err != nil {
		t.Fatal(err)
	}
	snapshots, err := r.Snapshots()
	if err != nil {
		t.Fatal(err)
	}
	id := snapshots[0].Files[0].Chunks[0].ID
	path, err := r.chunkPath(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("corrupted"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := r.Verify(""); err == nil {
		t.Fatal("Verify succeeded for a corrupted chunk")
	}
}

func TestPublicationFailuresDoNotExposePartialObjects(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}
	r.beforeChunkPublish = func() error { return errors.New("injected chunk crash") }
	if _, _, _, err := r.PutChunk([]byte("never published")); err == nil {
		t.Fatal("chunk publication unexpectedly succeeded")
	}
	idSum := sha256.Sum256([]byte("never published"))
	path, err := r.chunkPath(hex.EncodeToString(idSum[:]))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("partial chunk became visible")
	}
	r.beforeSnapshotPublish = func() error { return errors.New("injected snapshot crash") }
	m := model.Manifest{FormatVersion: model.FormatVersion, SnapshotID: "fault-test", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Files: []model.FileEntry{}}
	if err := r.publishManifest(m); err == nil {
		t.Fatal("snapshot publication unexpectedly succeeded")
	}
	if _, err := os.Stat(filepath.Join(r.snapshotsDir(), m.SnapshotID)); !os.IsNotExist(err) {
		t.Fatal("partial snapshot became visible")
	}
}

func TestParallelBackupPreservesChunkOrder(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i)
	}
	if err := os.WriteFile(filepath.Join(source, "large.bin"), data, 0644); err != nil {
		t.Fatal(err)
	}
	r, err := Open(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := r.BackupWithOptions(source, smallConfig(), BackupOptions{Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "restored")
	if err := r.Restore(snapshot.SnapshotID, destination); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(filepath.Join(destination, "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(data) {
		t.Fatal("parallel backup restored different bytes")
	}
}

func TestFileWorkerPipelineBacksUpManyFiles(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		if err := os.WriteFile(filepath.Join(source, fmt.Sprintf("file-%02d", i)), bytes.Repeat([]byte{byte(i)}, 128), 0644); err != nil {
			t.Fatal(err)
		}
	}
	r, err := Open(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, stats, err := r.BackupWithOptions(source, smallConfig(), BackupOptions{Workers: 1, FileWorkers: 4})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Files != 40 {
		t.Fatalf("files = %d, want 40", stats.Files)
	}
	if err := r.Restore(snapshot.SnapshotID, filepath.Join(root, "restored")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		if _, err := os.Stat(filepath.Join(root, "restored", fmt.Sprintf("file-%02d", i))); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBackupRejectsInvalidWorkerCount(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	r, err := Open(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.BackupWithOptions(source, smallConfig(), BackupOptions{}); err == nil {
		t.Fatal("accepted zero workers")
	}
}

func TestSelectiveRestore(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "keep.txt"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "skip.bin"), []byte("skip"), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := Open(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := r.Backup(source, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "restored")
	if err := r.RestoreWithOptions(snapshot.SnapshotID, destination, RestoreOptions{Includes: []string{"*.txt"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "keep.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "nested", "skip.bin")); !os.IsNotExist(err) {
		t.Fatal("restored an excluded file")
	}
}

func TestRestoreFileWritesVerifiedSingleFile(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	want := []byte("streamed contents")
	if err := os.WriteFile(filepath.Join(source, "nested", "file.txt"), want, 0644); err != nil {
		t.Fatal(err)
	}
	r, err := Open(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := r.Backup(source, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := r.RestoreFile(snapshot.SnapshotID, "nested/file.txt", &output); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("stdout content = %q, want %q", output.Bytes(), want)
	}
	if err := r.RestoreFile(snapshot.SnapshotID, "nested", &output); err == nil {
		t.Fatal("allowed a directory to be streamed")
	}
	if err := r.RestoreFile(snapshot.SnapshotID, "missing.txt", &output); err == nil {
		t.Fatal("allowed a missing file to be streamed")
	}
}

func TestFilesReferencingChunk(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	content := []byte("shared contents")
	for _, name := range []string{"one.txt", "two.txt"} {
		if err := os.WriteFile(filepath.Join(source, name), content, 0644); err != nil {
			t.Fatal(err)
		}
	}
	r, err := Open(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := r.Backup(source, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	files, err := r.FilesReferencingChunk(snapshot.SnapshotID, snapshot.Files[0].Chunks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Path != "one.txt" || files[1].Path != "two.txt" {
		t.Fatalf("unexpected chunk references: %+v", files)
	}
	if _, err := r.FilesReferencingChunk(snapshot.SnapshotID, "invalid"); err == nil {
		t.Fatal("accepted invalid chunk ID")
	}
}

func TestBackupRecordsLabels(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := Open(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := r.BackupWithOptions(source, smallConfig(), BackupOptions{Workers: 1, Labels: map[string]string{"environment": "test", "project": "vestige"}})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Labels["environment"] != "test" || snapshot.Labels["project"] != "vestige" {
		t.Fatalf("labels were not recorded: %+v", snapshot.Labels)
	}
	if _, _, err := r.BackupWithOptions(source, smallConfig(), BackupOptions{Workers: 1, Labels: map[string]string{"": "bad"}}); err == nil {
		t.Fatal("accepted invalid label")
	}
}

func TestRestoreOverwriteAndCleanDestination(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "restored.txt"), []byte("snapshot"), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := Open(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := r.Backup(source, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "destination")
	if err := os.Mkdir(destination, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "restored.txt"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "unrelated.txt"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := r.RestoreWithOptions(snapshot.SnapshotID, destination, RestoreOptions{Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "restored.txt"))
	if err != nil || string(got) != "snapshot" {
		t.Fatalf("overwrite result = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(destination, "unrelated.txt")); err != nil {
		t.Fatal("overwrite removed unrelated file")
	}
	if err := r.RestoreWithOptions(snapshot.SnapshotID, destination, RestoreOptions{CleanDestination: true}); err == nil {
		t.Fatal("clean destination did not require overwrite")
	}
	if err := r.RestoreWithOptions(snapshot.SnapshotID, destination, RestoreOptions{Overwrite: true, CleanDestination: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "unrelated.txt")); !os.IsNotExist(err) {
		t.Fatal("clean destination retained unrelated file")
	}
}

func TestCleanRestoreRefusesRepositoryAndSourceOverlap(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := Open(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := r.Backup(source, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{r.Root, source} {
		if err := r.RestoreWithOptions(snapshot.SnapshotID, destination, RestoreOptions{Overwrite: true, CleanDestination: true}); err == nil {
			t.Fatalf("allowed unsafe clean destination %s", destination)
		}
	}
}

func TestRestoreOverwritePreservesExistingFileOnChunkFailure(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("new snapshot data"), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := Open(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := r.Backup(source, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	chunkPath, err := r.chunkPath(snapshot.Files[0].Chunks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chunkPath, []byte("corrupt"), 0644); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "destination")
	if err := os.Mkdir(destination, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(destination, "file")
	if err := os.WriteFile(target, []byte("keep this"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := r.RestoreWithOptions(snapshot.SnapshotID, destination, RestoreOptions{Overwrite: true}); err == nil {
		t.Fatal("restored corrupt chunk")
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "keep this" {
		t.Fatalf("existing file was changed after failed overwrite: %q, %v", got, err)
	}
}

func TestGzipRepositoryRestoresPlaintextAndRecordsPhysicalBytes(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("highly compressible Vestige content\n"), 1000)
	if err := os.WriteFile(filepath.Join(source, "data.txt"), data, 0644); err != nil {
		t.Fatal(err)
	}
	r, err := OpenWithOptions(filepath.Join(root, "repo"), RepositoryOptions{Compression: "gzip"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, stats, err := r.Backup(source, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	if stats.BytesWritten >= int64(len(data)) {
		t.Fatalf("gzip wrote %d bytes for %d bytes of repetitive data", stats.BytesWritten, len(data))
	}
	if err := r.Verify(snapshot.SnapshotID); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "restored")
	if err := r.Restore(snapshot.SnapshotID, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "data.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("gzip repository restored different bytes")
	}
	if _, err := OpenWithOptions(filepath.Join(root, "repo"), RepositoryOptions{Compression: "none"}); err == nil {
		t.Fatal("allowed compression policy to change")
	}
}

func TestZstdRepositoryRestoresAndSkipsExpansion(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	random := make([]byte, 4096)
	for i := range random {
		random[i] = byte(i*37 + 11)
	}
	if err := os.WriteFile(filepath.Join(source, "random.bin"), random, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "text.txt"), bytes.Repeat([]byte("compress me\n"), 800), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := OpenWithOptions(filepath.Join(root, "repo"), RepositoryOptions{Compression: "zstd"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := r.Backup(source, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Verify(snapshot.SnapshotID); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "restored")
	if err := r.Restore(snapshot.SnapshotID, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "random.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, random) {
		t.Fatal("zstd repository restored different bytes")
	}
	firstID := snapshot.Files[0].Chunks[0].ID
	path, err := r.chunkPath(firstID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) < 5 || !bytes.Equal(stored[:5], []byte{'V', 'Z', '0', 1, 0}) {
		t.Fatal("incompressible zstd chunk was not stored raw")
	}
}

func TestGarbageCollectDryRunAndSweep(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("reachable"), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := Open(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Backup(source, smallConfig()); err != nil {
		t.Fatal(err)
	}
	orphanID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	orphanPath, err := r.chunkPath(orphanID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(orphanPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphanPath, []byte("orphan"), 0644); err != nil {
		t.Fatal(err)
	}
	dry, err := r.GarbageCollect(true, 0)
	if err != nil {
		t.Fatal(err)
	}
	if dry.OrphanChunks != 1 || dry.ReclaimedBytes != 6 {
		t.Fatalf("unexpected dry-run stats: %+v", dry)
	}
	if _, err := os.Stat(orphanPath); err != nil {
		t.Fatal("dry run removed orphan")
	}
	actual, err := r.GarbageCollect(false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if actual.OrphanChunks != 1 || actual.ReclaimedBytes != 6 {
		t.Fatalf("unexpected gc stats: %+v", actual)
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Fatal("gc did not remove orphan")
	}
}

func TestDeleteGCAndRestoreKeepsSharedChunks(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	shared := []byte("this chunk is deliberately shared between snapshots")
	if err := os.WriteFile(filepath.Join(source, "shared.txt"), shared, 0644); err != nil {
		t.Fatal(err)
	}
	r, err := Open(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := r.Backup(source, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "new.txt"), []byte("only in second snapshot"), 0644); err != nil {
		t.Fatal(err)
	}
	second, _, err := r.Backup(source, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.DeleteSnapshot(first.SnapshotID, false, 0); err != nil {
		t.Fatal(err)
	}
	preview, err := r.GarbageCollect(true, 0)
	if err != nil || preview.OrphanChunks != 0 {
		t.Fatalf("shared snapshot dry-run = %+v, %v", preview, err)
	}
	if _, err := r.GarbageCollect(false, 0); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "restored")
	if err := r.Restore(second.SnapshotID, destination); err != nil {
		t.Fatalf("restore surviving snapshot after delete and GC: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "shared.txt"))
	if err != nil || !bytes.Equal(got, shared) {
		t.Fatalf("restored shared content = %q, %v", got, err)
	}
}

func TestCheckReportsMissingAndOrphanChunks(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte("check me"), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := Open(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := r.Backup(source, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	chunkPath, err := r.chunkPath(snapshot.Files[0].Chunks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(chunkPath); err != nil {
		t.Fatal(err)
	}
	orphanID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	orphanPath, err := r.chunkPath(orphanID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(orphanPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphanPath, []byte("orphan"), 0644); err != nil {
		t.Fatal(err)
	}
	report, err := r.Check()
	if err != nil {
		t.Fatal(err)
	}
	if report.Healthy() || report.OrphanChunks != 1 || len(report.Issues) != 2 {
		t.Fatalf("unexpected check report: %+v", report)
	}
	kinds := map[string]bool{}
	for _, issue := range report.Issues {
		kinds[issue.Kind] = true
	}
	if !kinds["missing_chunk"] || !kinds["orphan_chunk"] {
		t.Fatalf("unexpected issues: %+v", report.Issues)
	}
}

func TestVerifyResumesFromCheckpoint(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "first.txt"), bytes.Repeat([]byte("a"), 128), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "second.txt"), bytes.Repeat([]byte("b"), 128), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := Open(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := r.Backup(source, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, entry := range snapshot.Files {
		for _, ref := range entry.Chunks {
			ids[ref.ID] = true
		}
	}
	if len(ids) < 2 {
		t.Fatal("test fixture did not produce multiple chunks")
	}
	var first string
	for id := range ids {
		first = id
		break
	}
	statePath := filepath.Join(root, "verify-state.json")
	if err := writeJSONAtomic(statePath, verifyState{Repository: r.Root, Selector: snapshot.SnapshotID, Verified: map[string]bool{first: true}}); err != nil {
		t.Fatal(err)
	}
	report, err := r.VerifyWithOptions(snapshot.SnapshotID, VerifyOptions{StatePath: statePath})
	if err != nil {
		t.Fatal(err)
	}
	if report.ResumedChunks != 1 || report.VerifiedChunks != len(ids)-1 {
		t.Fatalf("unexpected verification report: %+v", report)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatal("successful verification left a checkpoint behind")
	}
}

func TestOpenRejectsCorruptRepositoryConfig(t *testing.T) {
	root := t.TempDir()
	repoPath := filepath.Join(root, "repo")
	if _, err := Open(repoPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "config.json"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(repoPath); err == nil {
		t.Fatal("opened repository with corrupt config")
	}
}

func TestBackupLockAndExplicitStaleRecovery(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := Open(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	info := writeLockInfo{PID: 99999, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	b, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.lockPath(), b, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Backup(source, smallConfig()); err == nil {
		t.Fatal("backup proceeded despite active lock")
	}
	info.CreatedAt = time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano)
	b, err = json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.lockPath(), b, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.BackupWithOptions(source, smallConfig(), BackupOptions{Workers: 1, StaleLockAfter: time.Minute}); err != nil {
		t.Fatalf("did not recover stale lock: %v", err)
	}
	if _, err := os.Stat(r.lockPath()); !os.IsNotExist(err) {
		t.Fatal("backup did not release writer lock")
	}
}

func TestSafeJoinRejectsTraversal(t *testing.T) {
	base := t.TempDir()
	for _, path := range []string{"../outside", "/absolute", "nested/../../outside", "nested\\outside"} {
		if _, err := safeJoin(base, path); err == nil {
			t.Fatalf("accepted unsafe path %q", path)
		}
	}
}

func TestEncryptedRepositoryProtectsChunksAndManifests(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	repoPath := filepath.Join(root, "repo")
	destination := filepath.Join(root, "restored")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	secret := []byte("confidential backup content that must not reach disk in plaintext")
	if err := os.WriteFile(filepath.Join(source, "secret.txt"), secret, 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Init(repoPath, RepositoryOptions{Encrypt: true, Passphrase: "correct horse battery staple"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := r.Backup(source, smallConfig())
	if err != nil {
		t.Fatal(err)
	}

	manifestPath := filepath.Join(repoPath, "snapshots", snapshot.SnapshotID, "manifest.enc")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(manifest, []byte("secret.txt")) || bytes.Contains(manifest, secret) {
		t.Fatal("encrypted manifest contains plaintext metadata")
	}
	originalManifest := append([]byte(nil), manifest...)
	if _, err := os.Stat(filepath.Join(repoPath, "snapshots", snapshot.SnapshotID, "manifest.json")); !os.IsNotExist(err) {
		t.Fatal("encrypted repository wrote a plaintext manifest")
	}
	config, err := os.ReadFile(filepath.Join(repoPath, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(config, []byte("correct horse battery staple")) {
		t.Fatal("repository config persisted the passphrase")
	}
	chunkPath, err := r.chunkPath(snapshot.Files[0].Chunks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(chunkPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, secret) {
		t.Fatal("encrypted chunk contains plaintext")
	}

	if _, err := OpenWithOptions(repoPath, RepositoryOptions{Passphrase: "wrong"}); err == nil {
		t.Fatal("opened encrypted repository with wrong passphrase")
	}
	reopened, err := OpenWithOptions(repoPath, RepositoryOptions{Passphrase: "correct horse battery staple"})
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Verify(snapshot.SnapshotID); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Restore(snapshot.SnapshotID, destination); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(filepath.Join(destination, "secret.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored, secret) {
		t.Fatal("restored encrypted content differs")
	}
	manifest[len(manifest)-1] ^= 1
	if err := os.WriteFile(manifestPath, manifest, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ReadManifest(snapshot.SnapshotID); err == nil {
		t.Fatal("accepted a tampered encrypted manifest")
	}
	if err := os.WriteFile(manifestPath, originalManifest, 0600); err != nil {
		t.Fatal(err)
	}

	stored[len(stored)-1] ^= 1
	if err := os.WriteFile(chunkPath, stored, 0600); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Verify(snapshot.SnapshotID); err == nil {
		t.Fatal("verification accepted a tampered encrypted chunk")
	}
}

func TestRotatePassphraseRewrapsDataKey(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	repoPath := filepath.Join(root, "repo")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte("rotate without rewriting chunks"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Init(repoPath, RepositoryOptions{Encrypt: true, Passphrase: "old passphrase"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := r.Backup(source, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	chunkPath, err := r.chunkPath(snapshot.Files[0].Chunks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(chunkPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.RotatePassphrase("new passphrase", 0); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(chunkPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("rotation rewrote chunk: %v", err)
	}
	if _, err := OpenWithOptions(repoPath, RepositoryOptions{Passphrase: "old passphrase"}); err == nil {
		t.Fatal("old passphrase still opened repository")
	}
	reopened, err := OpenWithOptions(repoPath, RepositoryOptions{Passphrase: "new passphrase"})
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Verify(snapshot.SnapshotID); err != nil {
		t.Fatal(err)
	}
}

func TestRotatePassphraseUpgradesLegacyEncryptedRepository(t *testing.T) {
	root := t.TempDir()
	repoPath := filepath.Join(root, "repo")
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte("legacy rotation"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Open(repoPath)
	if err != nil {
		t.Fatal(err)
	}
	salt := bytes.Repeat([]byte{7}, 32)
	legacyKey, err := pbkdf2.Key(sha256.New, "old passphrase", salt, passphraseIterations, 32)
	if err != nil {
		t.Fatal(err)
	}
	check, err := seal(legacyKey, keyCheckPlaintext, keyCheckAAD)
	if err != nil {
		t.Fatal(err)
	}
	r.key = legacyKey
	r.config.Encryption = &model.EncryptionConfig{Algorithm: encryptionAlgorithm, KDF: passphraseKDF, Iterations: passphraseIterations, Salt: base64.StdEncoding.EncodeToString(salt), KeyCheck: base64.StdEncoding.EncodeToString(check)}
	configData, err := marshalJSON(r.config)
	if err != nil {
		t.Fatal(err)
	}
	_, version, err := r.store.Get("config.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.store.ReplaceIfVersion("config.json", configData, version); err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := r.Backup(source, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	chunkPath, err := r.chunkPath(snapshot.Files[0].Chunks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(chunkPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.RotatePassphrase("new passphrase", 0); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(chunkPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("legacy rotation rewrote chunk: %v", err)
	}
	reopened, err := OpenWithOptions(repoPath, RepositoryOptions{Passphrase: "new passphrase"})
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Verify(snapshot.SnapshotID); err != nil {
		t.Fatal(err)
	}
}

func TestReplicateEncryptedRepository(t *testing.T) {
	root := t.TempDir()
	sourceDir := filepath.Join(root, "source")
	if err := os.Mkdir(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "file.txt"), []byte("encrypted off-site copy"), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := Init(filepath.Join(root, "source-repo"), RepositoryOptions{Encrypt: true, Passphrase: "source passphrase"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := source.Backup(sourceDir, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	target, err := Init(filepath.Join(root, "target-repo"), RepositoryOptions{Encrypt: true, Passphrase: "target passphrase"})
	if err != nil {
		t.Fatal(err)
	}
	stats, err := source.ReplicateTo(target, 0)
	if err != nil {
		t.Fatal(err)
	}
	if stats.SnapshotsCopied != 1 || stats.ChunksCopied == 0 {
		t.Fatalf("unexpected replication stats: %+v", stats)
	}
	if err := target.Verify(snapshot.SnapshotID); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "restored")
	if err := target.Restore(snapshot.SnapshotID, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "file.txt"))
	if err != nil || string(got) != "encrypted off-site copy" {
		t.Fatalf("replicated restore = %q, %v", got, err)
	}
	second, err := source.ReplicateTo(target, 0)
	if err != nil || second.SnapshotsSkipped != 1 {
		t.Fatalf("repeat replication = %+v, %v", second, err)
	}
}

func TestRecoveryKitValidatesAwayFromRepository(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	repoPath := filepath.Join(root, "repo")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte("recovery metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Init(repoPath, RepositoryOptions{Encrypt: true, Passphrase: "kit passphrase"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Backup(source, smallConfig()); err != nil {
		t.Fatal(err)
	}
	kit := filepath.Join(root, "recovery-kit.zip")
	exported, err := r.ExportRecoveryKit(kit)
	if err != nil || exported.Snapshots != 1 {
		t.Fatalf("export recovery kit = %+v, %v", exported, err)
	}
	validated, err := ValidateRecoveryKit(kit, "kit passphrase")
	if err != nil || validated.Snapshots != 1 {
		t.Fatalf("validate recovery kit = %+v, %v", validated, err)
	}
	if _, err := ValidateRecoveryKit(kit, "wrong passphrase"); err == nil {
		t.Fatal("validated kit with wrong passphrase")
	}
}

func TestDisasterRecoveryDrillRestoresSnapshot(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte("disaster drill"), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := Open(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := r.Backup(source, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "restored")
	report, err := r.DisasterRecoveryDrill(snapshot.SnapshotID, destination)
	if err != nil || report.SnapshotID != snapshot.SnapshotID || report.Files != 1 || report.RPOSeconds < 0 || report.RTOSeconds < 0 {
		t.Fatalf("drill = %+v, %v", report, err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "file.txt"))
	if err != nil || string(got) != "disaster drill" {
		t.Fatalf("drill restore = %q, %v", got, err)
	}
}

func smallConfig() chunker.Config {
	return chunker.Config{Window: 8, MinSize: 4, TargetSize: 16, MaxSize: 64}
}
