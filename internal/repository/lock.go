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

type writeLock struct{ path string }

func (r *Repository) lockPath() string { return filepath.Join(r.Root, "write.lock") }

// acquireWriteLock serializes repository mutations. A stale lock is recovered
// only when the caller explicitly provides a positive lease duration; this
// avoids guessing whether a long-running backup is still alive.
func (r *Repository) acquireWriteLock(staleAfter time.Duration) (*writeLock, error) {
	path := r.lockPath()
	info := writeLockInfo{PID: os.Getpid(), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	b, err := json.Marshal(info)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			if _, writeErr := f.Write(append(b, '\n')); writeErr != nil {
				f.Close()
				os.Remove(path)
				return nil, writeErr
			}
			if closeErr := f.Close(); closeErr != nil {
				os.Remove(path)
				return nil, closeErr
			}
			return &writeLock{path: path}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if staleAfter <= 0 {
			return nil, fmt.Errorf("repository is locked by another writer (%s); retry later or use --stale-lock-after only after confirming the writer crashed", path)
		}
		stale, staleErr := lockIsStale(path, staleAfter)
		if staleErr != nil {
			return nil, staleErr
		}
		if !stale {
			return nil, fmt.Errorf("repository lock is newer than --stale-lock-after (%s)", staleAfter)
		}
		// Rename is recoverable evidence of the previous lease. The next loop
		// creates a fresh lock atomically rather than deleting a path blindly.
		quarantined := filepath.Join(r.tmpDir(), fmt.Sprintf("stale-lock-%d", time.Now().UnixNano()))
		if err := os.Rename(path, quarantined); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("quarantine stale repository lock: %w", err)
		}
	}
	return nil, errors.New("repository lock changed while attempting recovery; retry")
}

func lockIsStale(path string, maxAge time.Duration) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
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
	if err := os.Remove(l.path); err != nil && !errors.Is(err, os.ErrNotExist) {
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
