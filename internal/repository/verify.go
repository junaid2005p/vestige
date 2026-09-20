package repository

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/junaid/vestige/internal/model"
)

// VerifyOptions controls optional local progress persistence. StatePath is
// outside the repository, so verification does not modify repository data.
type VerifyOptions struct{ StatePath string }

// VerifyReport records verification work.
type VerifyReport struct {
	Snapshots      int `json:"snapshots"`
	Chunks         int `json:"chunks"`
	VerifiedChunks int `json:"verified_chunks"`
	ResumedChunks  int `json:"resumed_chunks"`
}

type verifyState struct {
	Repository string          `json:"repository"`
	Selector   string          `json:"selector"`
	Verified   map[string]bool `json:"verified_chunks"`
}

// VerifyWithOptions verifies every unique chunk referenced by snapshotID, or
// all snapshots when snapshotID is empty. With StatePath, every successfully
// verified chunk is checkpointed atomically and a later invocation with the
// same repository and selector resumes from that checkpoint. The checkpoint is
// removed only after a complete successful verification.
func (r *Repository) VerifyWithOptions(snapshotID string, options VerifyOptions) (VerifyReport, error) {
	var report VerifyReport
	manifests, err := r.verifyManifests(snapshotID)
	if err != nil {
		return report, err
	}
	report.Snapshots = len(manifests)
	ids := map[string]struct{}{}
	for _, m := range manifests {
		for _, entry := range m.Files {
			for _, ref := range entry.Chunks {
				ids[ref.ID] = struct{}{}
			}
		}
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	report.Chunks = len(ordered)
	state, err := r.loadVerifyState(snapshotID, options.StatePath)
	if err != nil {
		return report, err
	}
	for _, id := range ordered {
		if state.Verified[id] {
			report.ResumedChunks++
			continue
		}
		if err := r.verifyChunk(id); err != nil {
			return report, fmt.Errorf("verify chunk %s: %w", id, err)
		}
		report.VerifiedChunks++
		if options.StatePath != "" {
			state.Verified[id] = true
			if err := writeJSONAtomic(options.StatePath, state); err != nil {
				return report, fmt.Errorf("write verification checkpoint: %w", err)
			}
		}
	}
	if options.StatePath != "" {
		if err := os.Remove(options.StatePath); err != nil && !os.IsNotExist(err) {
			return report, fmt.Errorf("remove verification checkpoint: %w", err)
		}
	}
	return report, nil
}

func (r *Repository) verifyManifests(snapshotID string) ([]model.Manifest, error) {
	if snapshotID != "" {
		m, err := r.ReadManifest(snapshotID)
		if err != nil {
			return nil, err
		}
		return []model.Manifest{m}, nil
	}
	return r.Snapshots()
}

func (r *Repository) loadVerifyState(selector, statePath string) (verifyState, error) {
	state := verifyState{Repository: r.Root, Selector: selector, Verified: map[string]bool{}}
	if statePath == "" {
		return state, nil
	}
	b, err := os.ReadFile(statePath)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("read verification checkpoint: %w", err)
	}
	if err := json.Unmarshal(b, &state); err != nil {
		return state, fmt.Errorf("invalid verification checkpoint: %w", err)
	}
	if state.Repository != r.Root || state.Selector != selector {
		return state, fmt.Errorf("verification checkpoint belongs to a different repository or snapshot selector")
	}
	if state.Verified == nil {
		state.Verified = map[string]bool{}
	}
	return state, nil
}
