package repository

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DeleteStats describes a snapshot deletion preview or completed deletion.
// Chunks are deliberately not removed here; run GarbageCollect afterwards to
// reclaim only the chunks that no remaining snapshot reaches.
type DeleteStats struct {
	SnapshotID       string
	Files            int
	LogicalBytes     int64
	ReferencedChunks int
	DryRun           bool
}

// DeleteSnapshot removes one published manifest only after a caller opts out
// of dry-run. Moving the manifest directory out of snapshots is the atomic
// publication point: it is no longer visible to readers before cleanup starts.
func (r *Repository) DeleteSnapshot(id string, dryRun bool, staleAfter time.Duration) (DeleteStats, error) {
	stats := DeleteStats{SnapshotID: id, DryRun: dryRun}
	lock, err := r.acquireWriteLock(staleAfter)
	if err != nil {
		return stats, err
	}
	defer lock.release()
	if _, err := r.recoverStaging(); err != nil {
		return stats, err
	}
	manifest, err := r.ReadManifest(id)
	if err != nil {
		return stats, err
	}
	// "latest" is a selector, not an on-disk snapshot directory name. Resolve
	// it before calculating the removal path and reporting the affected ID.
	stats.SnapshotID = manifest.SnapshotID
	seen := make(map[string]struct{})
	for _, entry := range manifest.Files {
		if entry.Type != "file" {
			continue
		}
		stats.Files++
		stats.LogicalBytes += entry.Size
		for _, ref := range entry.Chunks {
			seen[ref.ID] = struct{}{}
		}
	}
	stats.ReferencedChunks = len(seen)
	if dryRun {
		return stats, nil
	}
	source := filepath.Join(r.snapshotsDir(), manifest.SnapshotID)
	trash := filepath.Join(r.tmpDir(), fmt.Sprintf("deleted-snapshot-%s-%d", manifest.SnapshotID, time.Now().UnixNano()))
	if err := os.Rename(source, trash); err != nil {
		return stats, fmt.Errorf("unpublish snapshot %s: %w", manifest.SnapshotID, err)
	}
	if err := os.RemoveAll(trash); err != nil {
		return stats, fmt.Errorf("remove unpublished snapshot %s: %w", manifest.SnapshotID, err)
	}
	return stats, nil
}
