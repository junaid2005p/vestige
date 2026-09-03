package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/junaid/vestige/internal/chunker"
	"github.com/junaid/vestige/internal/repository"
)

func runBackup(args []string) error {
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	workers := flags.Int("workers", 1, "number of chunk-store workers")
	compression := flags.String("compression", "none", "chunk compression: none or gzip")
	staleLockAfter := flags.Duration("stale-lock-after", 0, "explicitly recover a writer lock older than this duration (0 disables recovery)")
	description := flags.String("message", "", "snapshot description")
	tags := flags.String("tags", "", "comma-separated snapshot tags")
	labels := flags.String("labels", "", "comma-separated snapshot labels in key=value form")
	includes := flags.String("include", "", "comma-separated source-relative glob patterns")
	excludes := flags.String("exclude", "", "comma-separated source-relative glob patterns")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("usage: vestige backup [--workers N] <source-dir> <repo-dir>")
	}
	paths := flags.Args()
	if len(paths) != 2 || *workers < 1 || *staleLockAfter < 0 || (*compression != "none" && *compression != "gzip" && *compression != "zstd") {
		return fmt.Errorf("usage: vestige backup [--workers N] [--compression none|gzip|zstd] [--stale-lock-after DURATION] <source-dir> <repo-dir>")
	}
	if repoInsideSource(paths[1], paths[0]) {
		return fmt.Errorf("repository must not be inside the source directory")
	}
	labelMap, err := parseLabels(*labels)
	if err != nil {
		return err
	}
	r, err := repository.OpenWithOptions(paths[1], repository.RepositoryOptions{Compression: *compression})
	if err != nil {
		return err
	}
	start := time.Now()
	m, stats, err := r.BackupWithOptions(paths[0], chunker.DefaultConfig(), repository.BackupOptions{Workers: *workers, Description: *description, Tags: splitTags(*tags), Labels: labelMap, Includes: splitTags(*includes), Excludes: splitTags(*excludes), StaleLockAfter: *staleLockAfter})
	if err != nil {
		return err
	}
	fmt.Printf("snapshot: %s\nworkers: %d\ncompression: %s\nfiles: %d\nlogical bytes: %d\nchunks created: %d\nchunks reused: %d\nbytes written: %d\ncontent pipeline: %s\nelapsed: %s\n", m.SnapshotID, *workers, *compression, stats.Files, stats.LogicalBytes, stats.ChunksCreated, stats.ChunksReused, stats.BytesWritten, time.Duration(stats.ProcessingNanos).Round(time.Millisecond), time.Since(start).Round(time.Millisecond))
	return nil
}

func parseLabels(value string) (map[string]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	labels := make(map[string]string)
	for _, part := range strings.Split(value, ",") {
		key, labelValue, ok := strings.Cut(part, "=")
		key, labelValue = strings.TrimSpace(key), strings.TrimSpace(labelValue)
		if !ok || key == "" || labelValue == "" || strings.ContainsAny(key, "\r\n") || strings.ContainsAny(labelValue, "\r\n") {
			return nil, fmt.Errorf("invalid label %q (use key=value)", part)
		}
		if _, exists := labels[key]; exists {
			return nil, fmt.Errorf("duplicate label key %q", key)
		}
		labels[key] = labelValue
	}
	return labels, nil
}

func splitTags(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, tag := range parts {
		if tag = strings.TrimSpace(tag); tag != "" {
			out = append(out, tag)
		}
	}
	return out
}

func repoInsideSource(repo, source string) bool {
	repoAbs, err1 := filepath.Abs(repo)
	sourceAbs, err2 := filepath.Abs(source)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(sourceAbs, repoAbs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
