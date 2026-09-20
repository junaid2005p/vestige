package repository

import (
	"errors"
	"fmt"
	"reflect"
	"time"
)

// ReplicationStats describes objects copied to a second encrypted repository.
type ReplicationStats struct {
	SnapshotsCopied  int
	SnapshotsSkipped int
	ChunksCopied     int
	ChunksReused     int
}

// ReplicateTo copies every published snapshot to target. It reads plaintext
// only in memory, then lets the target repository compress and encrypt chunks
// with its own data key. A target manifest is published last, so a failed copy
// never exposes an incomplete snapshot.
func (r *Repository) ReplicateTo(target *Repository, staleAfter time.Duration) (ReplicationStats, error) {
	var stats ReplicationStats
	if target == nil || target.Root == r.Root {
		return stats, errors.New("source and target repositories must differ")
	}
	if !r.Encrypted() || !target.Encrypted() {
		return stats, errors.New("replication requires encrypted source and target repositories")
	}
	if r.Compression() != target.Compression() {
		return stats, fmt.Errorf("repository compression differs: source %s, target %s", r.Compression(), target.Compression())
	}
	lock, err := target.acquireWriteLock(staleAfter)
	if err != nil {
		return stats, err
	}
	defer lock.release()
	if _, err := target.recoverStaging(); err != nil {
		return stats, err
	}
	manifests, err := r.Snapshots()
	if err != nil {
		return stats, err
	}
	for _, manifest := range manifests {
		if existing, err := target.ReadManifest(manifest.SnapshotID); err == nil {
			if !reflect.DeepEqual(existing, manifest) {
				return stats, fmt.Errorf("target snapshot %s differs from source", manifest.SnapshotID)
			}
			stats.SnapshotsSkipped++
			continue
		} else if !errors.Is(err, errObjectNotFound) {
			return stats, err
		}
		seen := map[string]struct{}{}
		for _, entry := range manifest.Files {
			for _, ref := range entry.Chunks {
				if _, ok := seen[ref.ID]; ok {
					continue
				}
				seen[ref.ID] = struct{}{}
				plain, err := r.readChunk(ref.ID)
				if err != nil {
					return stats, fmt.Errorf("read source chunk %s: %w", ref.ID, err)
				}
				id, created, _, err := target.PutChunk(plain)
				if err != nil {
					return stats, fmt.Errorf("write target chunk %s: %w", ref.ID, err)
				}
				if id != ref.ID {
					return stats, fmt.Errorf("target chunk ID changed for %s", ref.ID)
				}
				if created {
					stats.ChunksCopied++
				} else {
					stats.ChunksReused++
				}
			}
		}
		if err := target.publishManifest(manifest); err != nil {
			return stats, err
		}
		stats.SnapshotsCopied++
	}
	return stats, nil
}
