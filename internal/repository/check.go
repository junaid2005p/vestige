package repository

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// CheckIssue describes one repository consistency problem.
// It contains object keys, not decrypted file contents.
type CheckIssue struct {
	Kind     string `json:"kind"`
	Object   string `json:"object"`
	Snapshot string `json:"snapshot,omitempty"`
	Detail   string `json:"detail"`
}

// CheckReport is the result of Check.
// Check does not modify the repository.
type CheckReport struct {
	FormatVersion    int          `json:"format_version"`
	Snapshots        int          `json:"snapshots"`
	ReferencedChunks int          `json:"referenced_chunks"`
	StoredChunks     int          `json:"stored_chunks"`
	OrphanChunks     int          `json:"orphan_chunks"`
	Issues           []CheckIssue `json:"issues"`
}

// Healthy reports whether all repository invariants checked by Check hold.
func (r CheckReport) Healthy() bool { return len(r.Issues) == 0 }

// Check scans every published manifest and chunk. It reports malformed
// manifests, missing or corrupt referenced chunks, and unreferenced chunks.
// Config validation happens when Open constructs the Repository, before Check
// can run; this prevents an invalid format from being mistaken for healthy.
func (r *Repository) Check() (CheckReport, error) {
	report := CheckReport{FormatVersion: r.config.FormatVersion}
	entries, err := r.store.List("snapshots/")
	if err != nil {
		return report, fmt.Errorf("list snapshots: %w", err)
	}
	live := map[string]struct{}{}
	for _, entry := range entries {
		parts := strings.Split(entry.Key, "/")
		if len(parts) != 3 || parts[0] != "snapshots" || parts[2] != r.manifestName() {
			continue
		}
		id := parts[1]
		manifest, readErr := r.ReadManifest(id)
		if readErr != nil {
			report.Issues = append(report.Issues, CheckIssue{Kind: "invalid_manifest", Object: entry.Key, Snapshot: id, Detail: readErr.Error()})
			continue
		}
		report.Snapshots++
		for _, file := range manifest.Files {
			for _, ref := range file.Chunks {
				live[ref.ID] = struct{}{}
			}
		}
	}
	report.ReferencedChunks = len(live)
	objects, err := r.store.List("chunks/")
	if err != nil {
		return report, fmt.Errorf("list chunks: %w", err)
	}
	stored := map[string]storedObject{}
	for _, object := range objects {
		id := path.Base(object.Key)
		if _, err := rChunkPath(id); err != nil {
			report.Issues = append(report.Issues, CheckIssue{Kind: "invalid_chunk_key", Object: object.Key, Detail: err.Error()})
			continue
		}
		stored[id] = object
		report.StoredChunks++
	}
	for id := range live {
		object, ok := stored[id]
		if !ok {
			report.Issues = append(report.Issues, CheckIssue{Kind: "missing_chunk", Object: "chunks/" + id, Detail: "referenced by a published manifest"})
			continue
		}
		if err := r.verifyChunk(id); err != nil {
			report.Issues = append(report.Issues, CheckIssue{Kind: "corrupt_chunk", Object: object.Key, Detail: err.Error()})
		}
	}
	for id, object := range stored {
		if _, ok := live[id]; !ok {
			report.OrphanChunks++
			report.Issues = append(report.Issues, CheckIssue{Kind: "orphan_chunk", Object: object.Key, Detail: "not referenced by any valid published manifest"})
		}
	}
	sort.Slice(report.Issues, func(i, j int) bool {
		if report.Issues[i].Object == report.Issues[j].Object {
			return report.Issues[i].Kind < report.Issues[j].Kind
		}
		return report.Issues[i].Object < report.Issues[j].Object
	})
	return report, nil
}
