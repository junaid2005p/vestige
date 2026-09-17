package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type writeLockInfo struct {
	PID       int    `json:"pid"`
	CreatedAt string `json:"created_at"`
}

type writeLock struct {
	store   objectStore
	key     string
	version string
}

func (r *Repository) lockPath() string { return filepath.Join(r.Root, "write.lock") }

// acquireWriteLock serializes repository mutations. A stale lock is recovered
// only when the caller explicitly provides a positive lease duration; this
// avoids guessing whether a long-running backup is still alive.
func (r *Repository) acquireWriteLock(staleAfter time.Duration) (*writeLock, error) {
	info := writeLockInfo{PID: os.Getpid(), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	b, err := json.Marshal(info)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		version, err := r.store.PutIfAbsent("write.lock", append(b, '\n'))
		if err == nil {
			return &writeLock{store: r.store, key: "write.lock", version: version}, nil
		}
		if !errors.Is(err, errObjectExists) {
			return nil, err
		}
		if staleAfter <= 0 {
			return nil, fmt.Errorf("repository is locked by another writer (%s); retry later or use --stale-lock-after only after confirming the writer crashed", r.Root)
		}
		lockData, lockVersion, getErr := r.store.Get("write.lock")
		if getErr != nil {
			if errors.Is(getErr, errObjectNotFound) {
				continue
			}
			return nil, getErr
		}
		stale, staleErr := lockDataIsStale(lockData, staleAfter)
		if staleErr != nil {
			return nil, staleErr
		}
		if !stale {
			return nil, fmt.Errorf("repository lock is newer than --stale-lock-after (%s)", staleAfter)
		}
		// S3 uses an ETag precondition here, so a stale-lock recovery cannot
		// delete a lock that another writer acquired after our read.
		if err := r.store.DeleteIfVersion("write.lock", lockVersion); err != nil {
			if errors.Is(err, errObjectChanged) || errors.Is(err, errObjectNotFound) {
				continue
			}
			return nil, fmt.Errorf("remove stale repository lock: %w", err)
		}
	}
	return nil, errors.New("repository lock changed while attempting recovery; retry")
}

func lockIsStale(path string, maxAge time.Duration) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return lockDataIsStale(b, maxAge)
}

func lockDataIsStale(b []byte, maxAge time.Duration) (bool, error) {
	var info writeLockInfo
	if err := json.Unmarshal(b, &info); err != nil {
		return false, fmt.Errorf("invalid repository lock; remove it manually only after confirming no writer is active: %w", err)
	}
	created, err := time.Parse(time.RFC3339Nano, info.CreatedAt)
	if err != nil {
		return false, fmt.Errorf("invalid repository lock timestamp: %w", err)
	}
	return time.Since(created) > maxAge, nil
}

func (l *writeLock) release() error {
	if l == nil {
		return nil
	}
	if err := l.store.DeleteIfVersion(l.key, l.version); err != nil && !errors.Is(err, errObjectNotFound) && !errors.Is(err, errObjectChanged) {
		return err
	}
	return nil
}

// Recover removes abandoned snapshot staging directories while holding the
// writer lock. It never changes published snapshots or chunks.
func (r *Repository) Recover(staleAfter time.Duration) (int, error) {
	lock, err := r.acquireWriteLock(staleAfter)
	if err != nil {
		return 0, err
	}
	defer lock.release()
	return r.recoverStaging()
}

func (r *Repository) recoverStaging() (int, error) {
	if r.remote {
		// Remote repositories publish a manifest object directly; incomplete work
		// has no visible staging prefix to recover.
		return 0, nil
	}
	entries, err := os.ReadDir(r.tmpDir())
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		isSnapshotStage := len(entry.Name()) >= len("snapshot-") && entry.Name()[:len("snapshot-")] == "snapshot-"
		isDeletedSnapshot := len(entry.Name()) >= len("deleted-snapshot-") && entry.Name()[:len("deleted-snapshot-")] == "deleted-snapshot-"
		if !entry.IsDir() || (!isSnapshotStage && !isDeletedSnapshot) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(r.tmpDir(), entry.Name())); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
