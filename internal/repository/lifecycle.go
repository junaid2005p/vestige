package repository

import (
	"fmt"
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
// of dry-run. A manifest is the publication point in both local and S3
// repositories, so deleting it immediately unpublishes the snapshot.
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
	if err := r.store.Delete(r.manifestKey(manifest.SnapshotID)); err != nil {
		return stats, fmt.Errorf("unpublish snapshot %s: %w", manifest.SnapshotID, err)
	}
	return stats, nil
}
