package repository

import (
	"fmt"
	"sort"
	"time"

	"github.com/junaid/vestige/internal/model"
)

// RetentionPolicy retains the newest snapshots in each requested time bucket.
// A non-positive setting disables that bucket.
type RetentionPolicy struct{ KeepLast, KeepDaily, KeepWeekly int }

// RetentionCandidates returns published snapshots that are not protected by
// the policy. It never mutates the repository, making it safe for previews.
func (r *Repository) RetentionCandidates(policy RetentionPolicy) ([]model.Manifest, error) {
	if policy.KeepLast < 0 || policy.KeepDaily < 0 || policy.KeepWeekly < 0 {
		return nil, fmt.Errorf("retention counts must not be negative")
	}
	snapshots, err := r.Snapshots()
	if err != nil {
		return nil, err
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].SnapshotID > snapshots[j].SnapshotID })
	keep := make(map[string]bool)
	days, weeks := make(map[string]bool), make(map[string]bool)
	dayCount, weekCount := 0, 0
	for index, snapshot := range snapshots {
		if index < policy.KeepLast {
			keep[snapshot.SnapshotID] = true
		}
		created, err := time.Parse(time.RFC3339Nano, snapshot.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("snapshot %s has invalid creation time: %w", snapshot.SnapshotID, err)
		}
		if policy.KeepDaily > 0 {
			key := created.UTC().Format("2006-01-02")
			if !days[key] && dayCount < policy.KeepDaily {
				days[key] = true
				dayCount++
				keep[snapshot.SnapshotID] = true
			}
		}
		if policy.KeepWeekly > 0 {
			year, week := created.UTC().ISOWeek()
			key := fmt.Sprintf("%04d-%02d", year, week)
			if !weeks[key] && weekCount < policy.KeepWeekly {
				weeks[key] = true
				weekCount++
				keep[snapshot.SnapshotID] = true
			}
		}
	}
	var remove []model.Manifest
	for _, snapshot := range snapshots {
		if !keep[snapshot.SnapshotID] {
			remove = append(remove, snapshot)
		}
	}
	return remove, nil
}
