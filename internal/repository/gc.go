package repository

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// GCStats is the result of a mark-and-sweep chunk collection.
type GCStats struct {
	ReachableChunks int
	OrphanChunks    int
	ReclaimedBytes  int64
	DryRun          bool
}

// GarbageCollect removes chunks that no published manifest references. It
// acquires the repository writer lock and first removes abandoned staging
// directories; dry-run performs exactly the same mark phase without deleting.
func (r *Repository) GarbageCollect(dryRun bool, staleAfter time.Duration) (GCStats, error) {
	var stats GCStats
	stats.DryRun = dryRun
	lock, err := r.acquireWriteLock(staleAfter)
	if err != nil {
		return stats, err
	}
	defer lock.release()
	if _, err := r.recoverStaging(); err != nil {
		return stats, err
	}
	manifests, err := r.Snapshots()
	if err != nil {
		return stats, err
	}
	live := make(map[string]struct{})
	for _, manifest := range manifests {
		for _, entry := range manifest.Files {
			for _, ref := range entry.Chunks {
				live[ref.ID] = struct{}{}
			}
		}
	}
	stats.ReachableChunks = len(live)
	err = filepath.WalkDir(r.chunksDir(), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		id := entry.Name()
		if _, ok := live[id]; ok {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		stats.OrphanChunks++
		stats.ReclaimedBytes += info.Size()
		if !dryRun {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("remove orphan chunk %s: %w", path, err)
			}
		}
		return nil
	})
	return stats, err
}
