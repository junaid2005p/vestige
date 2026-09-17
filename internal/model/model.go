package model

const FormatVersion = 1

// RepositoryConfig describes the on-disk repository format.
type RepositoryConfig struct {
	FormatVersion int               `json:"format_version"`
	Compression   string            `json:"compression"`
	Encryption    *EncryptionConfig `json:"encryption,omitempty"`
}

// EncryptionConfig contains the non-secret parameters needed to derive a
// repository key. The passphrase itself is never persisted.
type EncryptionConfig struct {
	Algorithm  string `json:"algorithm"`
	KDF        string `json:"kdf"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	KeyCheck   string `json:"key_check"`
}

// ChunkingConfig records the parameters necessary to interpret a snapshot.
type ChunkingConfig struct {
	Algorithm  string `json:"algorithm"`
	Window     int    `json:"window"`
	MinSize    int    `json:"min_size"`
	TargetSize int    `json:"target_size"`
	MaxSize    int    `json:"max_size"`
}

// ChunkRef identifies one consecutive piece of a file.
type ChunkRef struct {
	ID   string `json:"id"`
	Size int64  `json:"size"`
}

// FileEntry represents a regular file, directory, symbolic link, or hard link
// in a snapshot. LinkTarget is source-relative for hard links and is the exact
// link text returned by the operating system for symbolic links.
// Paths are slash-separated and relative to the backed-up source directory.
type FileEntry struct {
	Path       string     `json:"path"`
	Type       string     `json:"type"` // file or directory
	Size       int64      `json:"size"`
	Mode       uint32     `json:"mode"`
	ModifiedNS int64      `json:"modified_ns"`
	Chunks     []ChunkRef `json:"chunks,omitempty"`
	LinkTarget string     `json:"link_target,omitempty"`
}

// Manifest is the immutable commit record for a completed snapshot.
type Manifest struct {
	FormatVersion int               `json:"format_version"`
	SnapshotID    string            `json:"snapshot_id"`
	CreatedAt     string            `json:"created_at"`
	Source        string            `json:"source"`
	Description   string            `json:"description,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	Includes      []string          `json:"includes,omitempty"`
	Excludes      []string          `json:"excludes,omitempty"`
	Chunking      ChunkingConfig    `json:"chunking"`
	Files         []FileEntry       `json:"files"`
}

// BackupStats summarizes one backup operation.
type BackupStats struct {
	Files         int
	LogicalBytes  int64
	ChunksCreated int
	ChunksReused  int
	BytesWritten  int64
	// ProcessingNanos is elapsed time spent reading, chunking, hashing, and
	// storing file content. It deliberately excludes directory traversal and
	// manifest publication so profiler and benchmark output can distinguish the
	// content pipeline from metadata work.
	ProcessingNanos int64
}
