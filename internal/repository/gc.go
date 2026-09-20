package repository

import (
	"fmt"
	"path"
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
	objects, err := r.store.List("chunks/")
	if err != nil {
		return stats, err
	}
	for _, object := range objects {
		id := path.Base(object.Key)
		if _, ok := live[id]; ok {
			continue
		}
		stats.OrphanChunks++
		stats.ReclaimedBytes += object.Size
		if !dryRun {
			if err := r.store.Delete(object.Key); err != nil {
				return stats, fmt.Errorf("remove orphan chunk %s: %w", id, err)
			}
		}
	}
	return stats, nil
}
