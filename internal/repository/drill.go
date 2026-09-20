package repository

import (
	"fmt"
	"time"
)

// DrillReport records one full restore rehearsal. RPO is measured from the
// selected snapshot's creation time; RTO is the restore duration.
type DrillReport struct {
	SnapshotID string  `json:"snapshot_id"`
	Files      int     `json:"files"`
	RPOSeconds float64 `json:"rpo_seconds"`
	RTOSeconds float64 `json:"rto_seconds"`
}

// DisasterRecoveryDrill restores one selected snapshot using the same safe
// path as a normal restore, including chunk authentication and hashing.
func (r *Repository) DisasterRecoveryDrill(id, destination string) (DrillReport, error) {
	var report DrillReport
	manifest, err := r.ReadManifest(id)
	if err != nil {
		return report, err
	}
	created, err := time.Parse(time.RFC3339Nano, manifest.CreatedAt)
	if err != nil {
		return report, fmt.Errorf("invalid snapshot creation time: %w", err)
	}
	for _, entry := range manifest.Files {
		if entry.Type == "file" {
			report.Files++
		}
	}
	start := time.Now()
	if err := r.Restore(manifest.SnapshotID, destination); err != nil {
		return report, err
	}
	report.SnapshotID = manifest.SnapshotID
	report.RTOSeconds = time.Since(start).Seconds()
	report.RPOSeconds = time.Since(created).Seconds()
	if report.RPOSeconds < 0 {
		report.RPOSeconds = 0
	}
	return report, nil
}
