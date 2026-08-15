// Package repository implements Vestige's local on-disk repository.
package repository

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/junaid/vestige/internal/chunker"
	"github.com/junaid/vestige/internal/filter"
	"github.com/junaid/vestige/internal/model"
	"github.com/klauspost/compress/zstd"
)

type Repository struct {
	Root        string
	config      model.RepositoryConfig
	zstdEncoder *zstd.Encoder
	zstdDecoder *zstd.Decoder
}

// RepositoryOptions selects an immutable repository storage policy when a
// repository is created. An empty Compression accepts an existing policy.
type RepositoryOptions struct{ Compression string }

// Init creates a repository using the requested immutable storage policy.
// Calling it again validates and opens the existing repository.
func Init(root string, options RepositoryOptions) (*Repository, error) {
	return OpenWithOptions(root, options)
}

// BackupOptions controls execution without changing snapshot semantics.
type BackupOptions struct {
	// Workers is the number of concurrent chunk-store operations. It must be at
	// least one; use one for the deterministic baseline.
	Workers     int
	Description string
	Tags        []string
	Labels      map[string]string
	Includes    []string
	Excludes    []string
	// StaleLockAfter enables explicit recovery of a writer lock older than this
	// duration. Zero is the safe default: never reclaim a lock automatically.
	StaleLockAfter time.Duration
}

// RestoreOptions controls destination handling and filters source-relative
// entries restored from a snapshot. By default Restore refuses destinations
// that already exist. Overwrite makes replacing matching files explicit;
// CleanDestination additionally removes the destination's existing contents
// before restore and therefore requires Overwrite.
type RestoreOptions struct {
	Includes         []string
	Overwrite        bool
	CleanDestination bool
}

func Open(root string) (*Repository, error) {
	return OpenWithOptions(root, RepositoryOptions{})
}

func OpenWithOptions(root string, options RepositoryOptions) (*Repository, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if options.Compression != "" && options.Compression != "none" && options.Compression != "gzip" && options.Compression != "zstd" {
		return nil, fmt.Errorf("unsupported compression %q (choose none, gzip, or zstd)", options.Compression)
	}
	r := &Repository{Root: filepath.Clean(abs)}
	if err := os.MkdirAll(r.Root, 0755); err != nil {
		return nil, err
	}
	if err := r.ensureConfig(options.Compression); err != nil {
		return nil, err
	}
	if err := r.setupCodec(); err != nil {
		return nil, err
	}
	for _, dir := range []string{r.chunksDir(), r.snapshotsDir(), r.tmpDir()} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *Repository) configPath() string   { return filepath.Join(r.Root, "config.json") }
func (r *Repository) chunksDir() string    { return filepath.Join(r.Root, "chunks") }
func (r *Repository) snapshotsDir() string { return filepath.Join(r.Root, "snapshots") }
func (r *Repository) tmpDir() string       { return filepath.Join(r.Root, "tmp") }

func (r *Repository) ensureConfig(requestedCompression string) error {
	path := r.configPath()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if requestedCompression == "" {
			requestedCompression = "none"
		}
		r.config = model.RepositoryConfig{FormatVersion: model.FormatVersion, Compression: requestedCompression}
		return writeJSONAtomic(path, r.config)
	}
	if err != nil {
		return fmt.Errorf("read repository config: %w", err)
	}
	var config model.RepositoryConfig
	if err := json.Unmarshal(b, &config); err != nil {
		return fmt.Errorf("invalid repository config: %w", err)
	}
	if config.FormatVersion != model.FormatVersion {
		return fmt.Errorf("unsupported repository format version %d", config.FormatVersion)
	}
	if config.Compression == "" {
		config.Compression = "none"
	} // compatibility with early MVP repositories
	if config.Compression != "none" && config.Compression != "gzip" && config.Compression != "zstd" {
		return fmt.Errorf("unsupported repository compression %q", config.Compression)
	}
	if requestedCompression != "" && requestedCompression != config.Compression {
		return fmt.Errorf("repository uses %s compression; requested %s", config.Compression, requestedCompression)
	}
	r.config = config
	return nil
}

func (r *Repository) setupCodec() error {
	if r.config.Compression != "zstd" {
		return nil
	}
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		return err
	}
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		encoder.Close()
		return err
	}
	r.zstdEncoder, r.zstdDecoder = encoder, decoder
	return nil
}

func (r *Repository) chunkPath(id string) (string, error) {
	if len(id) != 64 {
		return "", fmt.Errorf("invalid chunk ID %q", id)
	}
	if _, err := hex.DecodeString(id); err != nil {
		return "", fmt.Errorf("invalid chunk ID %q", id)
	}
	return filepath.Join(r.chunksDir(), id[:2], id[2:4], id), nil
}

// PutChunk stores data exactly once. Existing chunks are rehashed before reuse.
func (r *Repository) PutChunk(data []byte) (id string, created bool, storedBytes int64, err error) {
	sum := sha256.Sum256(data)
	id = hex.EncodeToString(sum[:])
	path, err := r.chunkPath(id)
	if err != nil {
		return "", false, 0, err
	}
	if _, err = os.Stat(path); err == nil {
		if err := r.verifyChunk(id); err != nil {
			return "", false, 0, err
		}
		return id, false, 0, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", false, 0, err
	}
	stored, err := r.encodeChunk(data)
	if err != nil {
		return "", false, 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", false, 0, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".chunk-*")
	if err != nil {
		return "", false, 0, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(stored); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", false, 0, err
	}
	if err = os.Rename(tmpName, path); err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			if err := r.verifyChunk(id); err != nil {
				return "", false, 0, err
			}
			return id, false, 0, nil
		}
		return "", false, 0, err
	}
	return id, true, int64(len(stored)), nil
}

func (r *Repository) verifyChunk(id string) error {
	path, err := r.chunkPath(id)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open chunk %s: %w", id, err)
	}
	defer f.Close()
	plain, err := r.decodeChunk(f)
	if err != nil {
		return fmt.Errorf("read chunk %s: %w", id, err)
	}
	h := sha256.Sum256(plain)
	if actual := hex.EncodeToString(h[:]); actual != id {
		return fmt.Errorf("corrupt chunk %s (hash is %s)", id, actual)
	}
	return nil
}

func (r *Repository) readChunk(id string) ([]byte, error) {
	if err := r.verifyChunk(id); err != nil {
		return nil, err
	}
	path, _ := r.chunkPath(id)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return r.decodeChunk(f)
}

func (r *Repository) encodeChunk(plain []byte) ([]byte, error) {
	if r.config.Compression == "none" {
		return plain, nil
	}
	if r.config.Compression == "zstd" {
		compressed := r.zstdEncoder.EncodeAll(plain, nil)
		// A per-chunk envelope makes incompressible data cheaper than forcing a
		// compressed representation, while still making decoding unambiguous.
		if len(compressed) >= len(plain) {
			return append([]byte{'V', 'Z', '0', 1, 0}, plain...), nil
		}
		return append([]byte{'V', 'Z', '0', 1, 1}, compressed...), nil
	}
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	if _, err := w.Write(plain); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (r *Repository) decodeChunk(stored io.Reader) ([]byte, error) {
	if r.config.Compression == "none" {
		return io.ReadAll(stored)
	}
	if r.config.Compression == "zstd" {
		data, err := io.ReadAll(stored)
		if err != nil {
			return nil, err
		}
		if len(data) < 5 || string(data[:4]) != "VZ0\x01" {
			return nil, errors.New("invalid zstd chunk envelope")
		}
		switch data[4] {
		case 0:
			return append([]byte(nil), data[5:]...), nil
		case 1:
			return r.zstdDecoder.DecodeAll(data[5:], nil)
		default:
			return nil, errors.New("unknown zstd chunk representation")
		}
	}
	rd, err := gzip.NewReader(stored)
	if err != nil {
		return nil, err
	}
	defer rd.Close()
	return io.ReadAll(rd)
}

func (r *Repository) Backup(source string, cfg chunker.Config) (model.Manifest, model.BackupStats, error) {
	return r.BackupWithOptions(source, cfg, BackupOptions{Workers: 1})
}

// BackupWithOptions creates a snapshot. Worker count changes throughput only:
// chunk boundaries, chunk IDs, and manifest chunk order remain unchanged.
func (r *Repository) BackupWithOptions(source string, cfg chunker.Config, options BackupOptions) (model.Manifest, model.BackupStats, error) {
	var manifest model.Manifest
	var stats model.BackupStats
	if options.Workers < 1 {
		return manifest, stats, fmt.Errorf("workers must be at least 1")
	}
	lock, err := r.acquireWriteLock(options.StaleLockAfter)
	if err != nil {
		return manifest, stats, err
	}
	defer lock.release()
	if _, err := r.recoverStaging(); err != nil {
		return manifest, stats, err
	}
	if err := cfg.Validate(); err != nil {
		return manifest, stats, err
	}
	pathFilter, err := filter.New(options.Includes, options.Excludes)
	if err != nil {
		return manifest, stats, err
	}
	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return manifest, stats, err
	}
	sourceAbs = filepath.Clean(sourceAbs)
	info, err := os.Stat(sourceAbs)
	if err != nil {
		return manifest, stats, fmt.Errorf("source: %w", err)
	}
	if !info.IsDir() {
		return manifest, stats, fmt.Errorf("source must be a directory")
	}
	if isWithin(r.Root, sourceAbs) {
		return manifest, stats, fmt.Errorf("repository must not be inside the source directory")
	}

	now := time.Now().UTC()
	id := now.Format("20060102T150405.000000000Z")
	manifest = model.Manifest{
		FormatVersion: model.FormatVersion, SnapshotID: id, CreatedAt: now.Format(time.RFC3339Nano), Source: sourceAbs, Description: options.Description, Tags: options.Tags, Labels: options.Labels, Includes: options.Includes, Excludes: options.Excludes,
		Chunking: model.ChunkingConfig{Algorithm: chunker.Algorithm, Window: cfg.Window, MinSize: cfg.MinSize, TargetSize: cfg.TargetSize, MaxSize: cfg.MaxSize},
	}
	err = filepath.WalkDir(sourceAbs, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == sourceAbs {
			return nil
		}
		rel, err := filepath.Rel(sourceAbs, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if err := validatePath(rel); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsupported symlink: %s", rel)
		}
		fileInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if fileInfo.IsDir() {
			if pathFilter.Excluded(rel) {
				return filepath.SkipDir
			}
			manifest.Files = append(manifest.Files, model.FileEntry{Path: rel, Type: "directory", Mode: uint32(fileInfo.Mode().Perm()), ModifiedNS: fileInfo.ModTime().UnixNano()})
			return nil
		}
		if !pathFilter.Include(rel) {
			return nil
		}
		if !fileInfo.Mode().IsRegular() {
			return fmt.Errorf("unsupported file type: %s", rel)
		}
		before := fileInfo
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", rel, err)
		}
		entryOut := model.FileEntry{Path: rel, Type: "file", Size: before.Size(), Mode: uint32(before.Mode().Perm()), ModifiedNS: before.ModTime().UnixNano()}
		processStart := time.Now()
		refs, fileStats, splitErr := r.storeFileChunks(f, cfg, options.Workers)
		stats.ProcessingNanos += time.Since(processStart).Nanoseconds()
		entryOut.Chunks = refs
		stats.ChunksCreated += fileStats.ChunksCreated
		stats.ChunksReused += fileStats.ChunksReused
		stats.BytesWritten += fileStats.BytesWritten
		err = splitErr
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return fmt.Errorf("backup %s: %w", rel, err)
		}
		after, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat after reading %s: %w", rel, err)
		}
		if after.Size() != before.Size() || after.ModTime() != before.ModTime() {
			return fmt.Errorf("source file changed during backup: %s", rel)
		}
		manifest.Files = append(manifest.Files, entryOut)
		stats.Files++
		stats.LogicalBytes += before.Size()
		return nil
	})
	if err != nil {
		return model.Manifest{}, stats, err
	}
	if err := r.publishManifest(manifest); err != nil {
		return model.Manifest{}, stats, err
	}
	return manifest, stats, nil
}

type chunkJob struct {
	sequence int
	data     []byte
}

type chunkResult struct {
	sequence    int
	ref         model.ChunkRef
	created     bool
	storedBytes int64
	err         error
}

// storeFileChunks uses a bounded worker pool while retaining input order in
// the returned references. The channel capacity bounds in-flight chunk memory.
func (r *Repository) storeFileChunks(input io.Reader, cfg chunker.Config, workers int) ([]model.ChunkRef, model.BackupStats, error) {
	jobs := make(chan chunkJob, workers*2)
	results := make(chan chunkResult, workers*2)
	type collected struct {
		refs  map[int]model.ChunkRef
		stats model.BackupStats
		err   error
	}
	collectedResults := make(chan collected, 1)
	go func() {
		out := collected{refs: make(map[int]model.ChunkRef)}
		for result := range results {
			if result.err != nil {
				if out.err == nil {
					out.err = result.err
				}
				continue
			}
			out.refs[result.sequence] = result.ref
			if result.created {
				out.stats.ChunksCreated++
				out.stats.BytesWritten += result.storedBytes
			} else {
				out.stats.ChunksReused++
			}
		}
		collectedResults <- out
	}()
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				id, created, storedBytes, err := r.PutChunk(job.data)
				results <- chunkResult{sequence: job.sequence, ref: model.ChunkRef{ID: id, Size: int64(len(job.data))}, created: created, storedBytes: storedBytes, err: err}
			}
		}()
	}

	next := 0
	splitErr := chunker.Split(input, cfg, func(data []byte) error {
		jobs <- chunkJob{sequence: next, data: data}
		next++
		return nil
	})
	close(jobs)
	go func() { wg.Wait(); close(results) }()

	aggregate := <-collectedResults
	refs := make([]model.ChunkRef, next)
	for sequence, ref := range aggregate.refs {
		refs[sequence] = ref
	}
	if splitErr != nil {
		return nil, aggregate.stats, splitErr
	}
	if aggregate.err != nil {
		return nil, aggregate.stats, aggregate.err
	}
	return refs, aggregate.stats, nil
}

func (r *Repository) publishManifest(m model.Manifest) error {
	if err := validateManifest(m); err != nil {
		return err
	}
	tmp := filepath.Join(r.tmpDir(), "snapshot-"+m.SnapshotID)
	if err := os.Mkdir(tmp, 0755); err != nil {
		return fmt.Errorf("create snapshot staging area: %w", err)
	}
	defer os.RemoveAll(tmp)
	if err := writeJSONAtomic(filepath.Join(tmp, "manifest.json"), m); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(r.snapshotsDir(), m.SnapshotID)); err != nil {
		return fmt.Errorf("publish snapshot: %w", err)
	}
	return nil
}

func (r *Repository) ReadManifest(id string) (model.Manifest, error) {
	var m model.Manifest
	if strings.ContainsAny(id, `\\/`) || id == "" {
		return m, fmt.Errorf("invalid snapshot ID")
	}
	b, err := os.ReadFile(filepath.Join(r.snapshotsDir(), id, "manifest.json"))
	if err != nil {
		return m, fmt.Errorf("read snapshot %s: %w", id, err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("invalid manifest: %w", err)
	}
	if err := validateManifest(m); err != nil {
		return m, fmt.Errorf("invalid manifest: %w", err)
	}
	if m.SnapshotID != id {
		return m, fmt.Errorf("manifest snapshot ID does not match its path")
	}
	return m, nil
}

func (r *Repository) Snapshots() ([]model.Manifest, error) {
	entries, err := os.ReadDir(r.snapshotsDir())
	if err != nil {
		return nil, err
	}
	var snapshots []model.Manifest
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		m, err := r.ReadManifest(entry.Name())
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, m)
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].SnapshotID < snapshots[j].SnapshotID })
	return snapshots, nil
}

// FilesReferencingChunk returns the regular files in one snapshot that refer
// to id. It is useful for investigating deduplication and corruption reports.
func (r *Repository) FilesReferencingChunk(snapshotID, id string) ([]model.FileEntry, error) {
	if _, err := rChunkPath(id); err != nil {
		return nil, fmt.Errorf("invalid chunk ID %q", id)
	}
	m, err := r.ReadManifest(snapshotID)
	if err != nil {
		return nil, err
	}
	var files []model.FileEntry
	for _, entry := range m.Files {
		if entry.Type != "file" {
			continue
		}
		for _, ref := range entry.Chunks {
			if ref.ID == id {
				files = append(files, entry)
				break
			}
		}
	}
	return files, nil
}

func (r *Repository) Restore(id, destination string) error {
	return r.RestoreWithOptions(id, destination, RestoreOptions{})
}

// RestoreFile writes one source-relative regular file from a snapshot to
// output. It verifies every referenced chunk before writing it, so callers can
// safely use it for pipes without first restoring a destination tree.
func (r *Repository) RestoreFile(id, sourcePath string, output io.Writer) error {
	if err := validatePath(sourcePath); err != nil {
		return err
	}
	m, err := r.ReadManifest(id)
	if err != nil {
		return err
	}
	for _, entry := range m.Files {
		if entry.Path != sourcePath {
			continue
		}
		if entry.Type != "file" {
			return fmt.Errorf("snapshot path is not a regular file: %s", sourcePath)
		}
		var written int64
		for _, ref := range entry.Chunks {
			data, err := r.readChunk(ref.ID)
			if err != nil {
				return fmt.Errorf("restore %s: %w", entry.Path, err)
			}
			if int64(len(data)) != ref.Size {
				return fmt.Errorf("restore %s: chunk %s has unexpected size", entry.Path, ref.ID)
			}
			n, err := output.Write(data)
			written += int64(n)
			if err != nil {
				return fmt.Errorf("write %s: %w", entry.Path, err)
			}
			if n != len(data) {
				return fmt.Errorf("write %s: %w", entry.Path, io.ErrShortWrite)
			}
		}
		if written != entry.Size {
			return fmt.Errorf("restore %s: wrote %d bytes, expected %d", entry.Path, written, entry.Size)
		}
		return nil
	}
	return fmt.Errorf("snapshot path not found: %s", sourcePath)
}

func (r *Repository) RestoreWithOptions(id, destination string, options RestoreOptions) error {
	pathFilter, err := filter.New(options.Includes, nil)
	if err != nil {
		return err
	}
	m, err := r.ReadManifest(id)
	if err != nil {
		return err
	}
	dest, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	dest = filepath.Clean(dest)
	if options.CleanDestination && !options.Overwrite {
		return errors.New("clean destination requires overwrite")
	}
	info, statErr := os.Stat(dest)
	if statErr == nil {
		if !options.Overwrite {
			return fmt.Errorf("restore destination already exists: %s", dest)
		}
		if !info.IsDir() {
			return fmt.Errorf("restore destination is not a directory: %s", dest)
		}
		if options.CleanDestination {
			if err := r.validateCleanDestination(dest, m.Source); err != nil {
				return err
			}
			if err := cleanDirectory(dest); err != nil {
				return fmt.Errorf("clean restore destination: %w", err)
			}
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	} else if options.CleanDestination {
		return fmt.Errorf("clean restore destination must already exist: %s", dest)
	}
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	var restoredDirectories []struct {
		path  string
		entry model.FileEntry
	}
	for _, entry := range m.Files {
		if entry.Type == "file" && !pathFilter.Include(entry.Path) {
			continue
		}
		if entry.Type == "directory" && len(options.Includes) > 0 {
			continue
		}
		path, err := safeJoin(dest, entry.Path)
		if err != nil {
			return err
		}
		if entry.Type == "directory" {
			if err := os.MkdirAll(path, fs.FileMode(entry.Mode)); err != nil {
				return err
			}
			restoredDirectories = append(restoredDirectories, struct {
				path  string
				entry model.FileEntry
			}{path: path, entry: entry})
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		writePath := path
		var temporary bool
		var f *os.File
		if options.Overwrite {
			f, err = os.CreateTemp(filepath.Dir(path), ".vestige-restore-*")
			if err == nil {
				temporary = true
				writePath = f.Name()
				err = f.Chmod(fs.FileMode(entry.Mode))
			}
		} else {
			f, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fs.FileMode(entry.Mode))
		}
		if err != nil {
			if f != nil {
				f.Close()
			}
			if temporary {
				os.Remove(writePath)
			}
			return err
		}
		var written int64
		for _, ref := range entry.Chunks {
			data, err := r.readChunk(ref.ID)
			if err != nil {
				f.Close()
				if temporary {
					os.Remove(writePath)
				}
				return fmt.Errorf("restore %s: %w", entry.Path, err)
			}
			if int64(len(data)) != ref.Size {
				f.Close()
				if temporary {
					os.Remove(writePath)
				}
				return fmt.Errorf("restore %s: chunk %s has unexpected size", entry.Path, ref.ID)
			}
			n, err := f.Write(data)
			written += int64(n)
			if err != nil {
				f.Close()
				if temporary {
					os.Remove(writePath)
				}
				return err
			}
		}
		if err := f.Close(); err != nil {
			if temporary {
				os.Remove(writePath)
			}
			return err
		}
		if written != entry.Size {
			if temporary {
				os.Remove(writePath)
			}
			return fmt.Errorf("restore %s: wrote %d bytes, expected %d", entry.Path, written, entry.Size)
		}
		if err := applyMetadata(writePath, entry); err != nil {
			if temporary {
				os.Remove(writePath)
			}
			return fmt.Errorf("restore metadata for %s: %w", entry.Path, err)
		}
		if temporary {
			if err := replaceFile(writePath, path); err != nil {
				os.Remove(writePath)
				return fmt.Errorf("replace restored file %s: %w", entry.Path, err)
			}
		}
	}
	// Apply directory metadata after all children are written because child
	// creation changes a directory's modification time on most filesystems.
	for i := len(restoredDirectories) - 1; i >= 0; i-- {
		dir := restoredDirectories[i]
		if err := applyMetadata(dir.path, dir.entry); err != nil {
			return fmt.Errorf("restore metadata for %s: %w", dir.entry.Path, err)
		}
	}
	return nil
}

func replaceFile(source, destination string) error {
	if err := os.Rename(source, destination); err == nil {
		return nil
	}
	// Windows does not replace an existing destination with Rename. The staged
	// file is complete and verified before this point, so removal is deferred
	// until the final replacement step.
	if err := os.Remove(destination); err != nil {
		return err
	}
	return os.Rename(source, destination)
}

func (r *Repository) validateCleanDestination(destination, source string) error {
	volumeRoot := filepath.VolumeName(destination) + string(filepath.Separator)
	if filepath.Clean(destination) == volumeRoot {
		return fmt.Errorf("refusing to clean filesystem root: %s", destination)
	}
	// Cleaning a repository, a parent of one, or the original source risks
	// destroying the only backup copy or the source being restored.
	if pathsOverlap(destination, r.Root) {
		return fmt.Errorf("refusing to clean destination overlapping repository: %s", destination)
	}
	if source != "" && pathsOverlap(destination, source) {
		return fmt.Errorf("refusing to clean destination overlapping snapshot source: %s", destination)
	}
	return nil
}

func pathsOverlap(a, b string) bool { return isWithin(a, b) || isWithin(b, a) }

func cleanDirectory(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func applyMetadata(path string, entry model.FileEntry) error {
	if err := os.Chmod(path, fs.FileMode(entry.Mode)); err != nil {
		return err
	}
	modified := time.Unix(0, entry.ModifiedNS)
	return os.Chtimes(path, modified, modified)
}

func (r *Repository) Verify(snapshotID string) error {
	var manifests []model.Manifest
	if snapshotID != "" {
		m, err := r.ReadManifest(snapshotID)
		if err != nil {
			return err
		}
		manifests = []model.Manifest{m}
	} else {
		var err error
		manifests, err = r.Snapshots()
		if err != nil {
			return err
		}
	}
	for _, m := range manifests {
		for _, entry := range m.Files {
			for _, ref := range entry.Chunks {
				if err := r.verifyChunk(ref.ID); err != nil {
					return fmt.Errorf("snapshot %s, file %s: %w", m.SnapshotID, entry.Path, err)
				}
			}
		}
	}
	return nil
}

// Stats reports physical chunk bytes and logical bytes across all snapshots.
// Logical bytes intentionally counts each snapshot's files, so it is the
// appropriate denominator for repository-wide deduplication measurements.
func (r *Repository) Stats() (chunks int, physicalBytes int64, snapshots int, logicalBytes int64, err error) {
	list, err := r.Snapshots()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	snapshots = len(list)
	for _, manifest := range list {
		for _, entry := range manifest.Files {
			if entry.Type == "file" {
				logicalBytes += entry.Size
			}
		}
	}
	err = filepath.WalkDir(r.chunksDir(), func(path string, e fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !e.IsDir() {
			i, err := e.Info()
			if err != nil {
				return err
			}
			chunks++
			physicalBytes += i.Size()
		}
		return nil
	})
	return
}

func validateManifest(m model.Manifest) error {
	if m.FormatVersion != model.FormatVersion || m.SnapshotID == "" {
		return fmt.Errorf("unsupported or incomplete manifest")
	}
	seen := map[string]struct{}{}
	for key, value := range m.Labels {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" || strings.ContainsAny(key, "=\r\n") || strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("invalid snapshot label %q", key)
		}
	}
	for _, e := range m.Files {
		if err := validatePath(e.Path); err != nil {
			return err
		}
		if _, exists := seen[e.Path]; exists {
			return fmt.Errorf("duplicate manifest path %q", e.Path)
		}
		seen[e.Path] = struct{}{}
		if e.Type != "file" && e.Type != "directory" {
			return fmt.Errorf("invalid entry type for %q", e.Path)
		}
		if e.Type == "file" {
			var total int64
			for _, ref := range e.Chunks {
				if _, err := rChunkPath(ref.ID); err != nil || ref.Size < 0 {
					return fmt.Errorf("invalid chunk in %q", e.Path)
				}
				total += ref.Size
			}
			if total != e.Size {
				return fmt.Errorf("incorrect size for %q", e.Path)
			}
		}
	}
	return nil
}

func rChunkPath(id string) (string, error) {
	if len(id) != 64 {
		return "", errors.New("invalid chunk ID")
	}
	_, err := hex.DecodeString(id)
	return id, err
}
func validatePath(path string) error {
	if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, `\`) {
		return fmt.Errorf("unsafe manifest path %q", path)
	}
	for _, p := range strings.Split(path, "/") {
		if p == "" || p == "." || p == ".." {
			return fmt.Errorf("unsafe manifest path %q", path)
		}
	}
	return nil
}
func safeJoin(base, rel string) (string, error) {
	if err := validatePath(rel); err != nil {
		return "", err
	}
	out := filepath.Join(base, filepath.FromSlash(rel))
	if !isWithin(out, base) {
		return "", fmt.Errorf("restore path escapes destination: %q", rel)
	}
	return out, nil
}
func isWithin(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func writeJSONAtomic(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
