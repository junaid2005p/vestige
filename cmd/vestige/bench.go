package main

// The benchmark command deliberately creates all of its data under one unique
// child of --workdir. It never removes --workdir itself.

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/junaid/vestige/internal/chunker"
	"github.com/junaid/vestige/internal/repository"
)

const mib = int64(1024 * 1024)

type benchRun struct {
	Trial                  int     `json:"trial"`
	DatasetBytes           int64   `json:"dataset_bytes"`
	InitialBackupSeconds   float64 `json:"initial_backup_seconds"`
	InitialBackupMiBPerSec float64 `json:"initial_backup_mib_per_second"`
	InitialCreatedChunks   int     `json:"initial_created_chunks"`
	InitialWrittenBytes    int64   `json:"initial_written_bytes"`
	IncrementalSeconds     float64 `json:"incremental_backup_seconds"`
	IncrementalMiBPerSec   float64 `json:"incremental_backup_mib_per_second"`
	IncrementalReusePct    float64 `json:"incremental_chunk_reuse_percent"`
	IncrementalWritten     int64   `json:"incremental_written_bytes"`
	VerifySeconds          float64 `json:"verify_seconds"`
	RestoreSeconds         float64 `json:"restore_seconds"`
	RestoreMiBPerSec       float64 `json:"restore_mib_per_second"`
	RepositoryBytes        int64   `json:"repository_bytes"`
	LogicalSnapshotBytes   int64   `json:"logical_snapshot_bytes"`
	SpaceSavedPct          float64 `json:"space_saved_percent"`
	AllocatedBytes         uint64  `json:"go_allocated_bytes"`
	CPUProfile             string  `json:"cpu_profile,omitempty"`
	HeapProfile            string  `json:"heap_profile,omitempty"`
}

type benchResult struct {
	Format           string       `json:"format"`
	CreatedAt        string       `json:"created_at"`
	Command          string       `json:"command"`
	GoVersion        string       `json:"go_version"`
	OS               string       `json:"os"`
	Architecture     string       `json:"architecture"`
	CPUs             int          `json:"logical_cpus"`
	DatasetMiB       int          `json:"dataset_mib"`
	Trials           int          `json:"trials"`
	Workers          int          `json:"workers"`
	Compression      string       `json:"compression"`
	ChangePercent    int          `json:"change_percent"`
	DiskBudgetMiB    int          `json:"disk_budget_mib"`
	EstimatedPeakMiB int          `json:"estimated_peak_mib"`
	Runs             []benchRun   `json:"runs"`
	Summary          benchSummary `json:"summary"`
}

type metricSummary struct {
	Min    float64 `json:"min"`
	Median float64 `json:"median"`
	Max    float64 `json:"max"`
}

type benchSummary struct {
	InitialBackupMiBPerSec metricSummary `json:"initial_backup_mib_per_second"`
	IncrementalMiBPerSec   metricSummary `json:"incremental_backup_mib_per_second"`
	IncrementalReusePct    metricSummary `json:"incremental_chunk_reuse_percent"`
	VerifySeconds          metricSummary `json:"verify_seconds"`
	RestoreMiBPerSec       metricSummary `json:"restore_mib_per_second"`
	SpaceSavedPct          metricSummary `json:"space_saved_percent"`
}

func runBench(args []string) error {
	flags := flag.NewFlagSet("bench", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	workdir := flags.String("workdir", "", "parent directory for temporary benchmark data")
	datasetMiB := flags.Int("dataset-mib", 128, "generated source size in MiB")
	trials := flags.Int("trials", 3, "number of independent trials")
	workers := flags.Int("workers", 1, "backup workers")
	compression := flags.String("compression", "none", "none, gzip, or zstd")
	changePercent := flags.Int("change-percent", 10, "files changed before incremental backup")
	diskBudgetMiB := flags.Int("max-disk-mib", 1024, "maximum permitted estimated peak disk use")
	output := flags.String("output", "", "new JSON result path; also writes a CSV beside it")
	keep := flags.Bool("keep", false, "keep generated data directories for inspection")
	profileDir := flags.String("profile-dir", "", "directory for per-trial CPU and heap pprof files")
	if err := flags.Parse(args); err != nil || len(flags.Args()) != 0 || *workdir == "" || *datasetMiB < 8 || *trials < 1 || *workers < 1 || *changePercent < 1 || *changePercent > 100 || (*compression != "none" && *compression != "gzip" && *compression != "zstd") || *diskBudgetMiB < 1 {
		return errors.New("usage: vestige bench --workdir <parent-dir> [--dataset-mib 128] [--trials 3] [--workers 1] [--compression none|gzip|zstd] [--change-percent 10] [--max-disk-mib 1024] [--profile-dir profiles] [--output results.json] [--keep]")
	}
	// Source + restore + repository (including the changed chunks) + generous metadata headroom.
	estimated := 3*(*datasetMiB) + 64
	if estimated > *diskBudgetMiB {
		return fmt.Errorf("estimated peak use is %d MiB, above --max-disk-mib %d; reduce --dataset-mib or explicitly raise the budget", estimated, *diskBudgetMiB)
	}
	if *output != "" {
		if _, err := os.Stat(*output); err == nil {
			return fmt.Errorf("refusing to overwrite result file %s", *output)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		csvPath := strings.TrimSuffix(*output, filepath.Ext(*output)) + ".csv"
		if _, err := os.Stat(csvPath); err == nil {
			return fmt.Errorf("refusing to overwrite CSV result file %s", csvPath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	base, err := filepath.Abs(*workdir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(base, 0755); err != nil {
		return err
	}
	if *profileDir != "" {
		if err := os.MkdirAll(*profileDir, 0755); err != nil {
			return err
		}
	}
	result := benchResult{Format: "vestige-benchmark/v1", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Command: "vestige " + strings.Join(os.Args[1:], " "), GoVersion: runtime.Version(), OS: runtime.GOOS, Architecture: runtime.GOARCH, CPUs: runtime.NumCPU(), DatasetMiB: *datasetMiB, Trials: *trials, Workers: *workers, Compression: *compression, ChangePercent: *changePercent, DiskBudgetMiB: *diskBudgetMiB, EstimatedPeakMiB: estimated}
	for trial := 1; trial <= *trials; trial++ {
		run, err := executeBenchTrial(base, trial, *datasetMiB, *workers, *compression, *changePercent, *keep, *profileDir)
		if err != nil {
			return fmt.Errorf("benchmark trial %d: %w", trial, err)
		}
		result.Runs = append(result.Runs, run)
	}
	result.Summary = summarizeBenchRuns(result.Runs)
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if *output == "" {
		fmt.Println(string(b))
		return nil
	}
	f, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := writeBenchCSV(strings.TrimSuffix(*output, filepath.Ext(*output))+".csv", result.Runs); err != nil {
		return err
	}
	fmt.Printf("benchmark complete: %s\n", *output)
	return nil
}

func summarizeBenchRuns(runs []benchRun) benchSummary {
	initial, incremental, reuse, verify, restore, saved := make([]float64, 0, len(runs)), make([]float64, 0, len(runs)), make([]float64, 0, len(runs)), make([]float64, 0, len(runs)), make([]float64, 0, len(runs)), make([]float64, 0, len(runs))
	for _, r := range runs {
		initial = append(initial, r.InitialBackupMiBPerSec)
		incremental = append(incremental, r.IncrementalMiBPerSec)
		reuse = append(reuse, r.IncrementalReusePct)
		verify = append(verify, r.VerifySeconds)
		restore = append(restore, r.RestoreMiBPerSec)
		saved = append(saved, r.SpaceSavedPct)
	}
	return benchSummary{InitialBackupMiBPerSec: summarizeMetric(initial), IncrementalMiBPerSec: summarizeMetric(incremental), IncrementalReusePct: summarizeMetric(reuse), VerifySeconds: summarizeMetric(verify), RestoreMiBPerSec: summarizeMetric(restore), SpaceSavedPct: summarizeMetric(saved)}
}

func summarizeMetric(values []float64) metricSummary {
	sort.Float64s(values)
	if len(values) == 0 {
		return metricSummary{}
	}
	median := values[len(values)/2]
	if len(values)%2 == 0 {
		median = (values[len(values)/2-1] + values[len(values)/2]) / 2
	}
	return metricSummary{Min: values[0], Median: median, Max: values[len(values)-1]}
}

func executeBenchTrial(base string, trial, sizeMiB, workers int, compression string, changePercent int, keep bool, profileDir string) (benchRun, error) {
	root, err := os.MkdirTemp(base, "vestige-bench-")
	if err != nil {
		return benchRun{}, err
	}
	if !keep {
		defer os.RemoveAll(root)
	}
	source, repoPath, restored := filepath.Join(root, "source"), filepath.Join(root, "repo"), filepath.Join(root, "restored")
	paths, bytes, err := generateBenchDataset(source, sizeMiB, uint64(trial))
	if err != nil {
		return benchRun{}, err
	}
	r, err := repository.Init(repoPath, repository.RepositoryOptions{Compression: compression})
	if err != nil {
		return benchRun{}, err
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var cpuProfilePath, heapProfilePath string
	var cpuProfile *os.File
	if profileDir != "" {
		cpuProfilePath = filepath.Join(profileDir, fmt.Sprintf("trial-%02d-initial-backup.cpu.pprof", trial))
		cpuProfile, err = os.OpenFile(cpuProfilePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return benchRun{}, err
		}
		if err := pprof.StartCPUProfile(cpuProfile); err != nil {
			cpuProfile.Close()
			return benchRun{}, err
		}
	}
	start := time.Now()
	_, first, err := r.BackupWithOptions(source, chunker.DefaultConfig(), repository.BackupOptions{Workers: workers})
	initial := time.Since(start)
	if cpuProfile != nil {
		pprof.StopCPUProfile()
		if closeErr := cpuProfile.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		return benchRun{}, err
	}
	if err := mutateBenchDataset(paths, changePercent, uint64(trial)); err != nil {
		return benchRun{}, err
	}
	start = time.Now()
	secondManifest, second, err := r.BackupWithOptions(source, chunker.DefaultConfig(), repository.BackupOptions{Workers: workers})
	incremental := time.Since(start)
	if err != nil {
		return benchRun{}, err
	}
	start = time.Now()
	err = r.Verify("")
	verify := time.Since(start)
	if err != nil {
		return benchRun{}, err
	}
	start = time.Now()
	err = r.Restore(secondManifest.SnapshotID, restored)
	restore := time.Since(start)
	if err != nil {
		return benchRun{}, err
	}
	if err := compareBenchTrees(source, restored); err != nil {
		return benchRun{}, err
	}
	chunks, physical, _, logical, err := r.Stats()
	_ = chunks
	if err != nil {
		return benchRun{}, err
	}
	runtime.ReadMemStats(&after)
	if profileDir != "" {
		runtime.GC()
		heapProfilePath = filepath.Join(profileDir, fmt.Sprintf("trial-%02d-after-restore.heap.pprof", trial))
		heap, profileErr := os.OpenFile(heapProfilePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if profileErr != nil {
			return benchRun{}, profileErr
		}
		profileErr = pprof.WriteHeapProfile(heap)
		closeErr := heap.Close()
		if profileErr != nil {
			return benchRun{}, profileErr
		}
		if closeErr != nil {
			return benchRun{}, closeErr
		}
	}
	reuse := 0.0
	if total := second.ChunksCreated + second.ChunksReused; total > 0 {
		reuse = float64(second.ChunksReused) * 100 / float64(total)
	}
	saved := 0.0
	if logical > 0 {
		saved = float64(logical-physical) * 100 / float64(logical)
	}
	if keep {
		fmt.Printf("trial %d retained at: %s\n", trial, root)
	}
	return benchRun{Trial: trial, DatasetBytes: bytes, InitialBackupSeconds: initial.Seconds(), InitialBackupMiBPerSec: throughput(bytes, initial), InitialCreatedChunks: first.ChunksCreated, InitialWrittenBytes: first.BytesWritten, IncrementalSeconds: incremental.Seconds(), IncrementalMiBPerSec: throughput(bytes, incremental), IncrementalReusePct: reuse, IncrementalWritten: second.BytesWritten, VerifySeconds: verify.Seconds(), RestoreSeconds: restore.Seconds(), RestoreMiBPerSec: throughput(bytes, restore), RepositoryBytes: physical, LogicalSnapshotBytes: logical, SpaceSavedPct: saved, AllocatedBytes: after.TotalAlloc - before.TotalAlloc, CPUProfile: cpuProfilePath, HeapProfile: heapProfilePath}, nil
}

func throughput(bytes int64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(bytes) / float64(mib) / d.Seconds()
}

func generateBenchDataset(root string, sizeMiB int, seed uint64) ([]string, int64, error) {
	var paths []string
	for i := 0; i < sizeMiB; i++ {
		dir := filepath.Join(root, fmt.Sprintf("set-%02d", i%16))
		path := filepath.Join(dir, fmt.Sprintf("file-%04d.bin", i))
		var data []byte
		switch i % 5 {
		case 0:
			data = benchTextBlock(i)
		case 1:
			data = benchRepeatedBlock()
		default:
			data = benchRandomBlock(seed + uint64(i)*0x9e3779b97f4a7c15)
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, 0, err
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			return nil, 0, err
		}
		paths = append(paths, path)
	}
	return paths, int64(sizeMiB) * mib, nil
}

func benchRandomBlock(seed uint64) []byte {
	b := make([]byte, mib)
	for i := range b {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		b[i] = byte(seed)
	}
	return b
}
func benchRepeatedBlock() []byte {
	b := make([]byte, mib)
	for i := range b {
		b[i] = byte((i / 4096) % 17)
	}
	return b
}
func benchTextBlock(n int) []byte {
	return []byte(strings.Repeat(fmt.Sprintf("vestige benchmark text corpus %04d\n", n), int(mib)/len(fmt.Sprintf("vestige benchmark text corpus %04d\n", n))+1)[:mib])
}

func mutateBenchDataset(paths []string, percent int, seed uint64) error {
	count := (len(paths)*percent + 99) / 100
	for i := 0; i < count; i++ {
		if err := os.WriteFile(paths[i], benchRandomBlock(seed+0xd1b54a32d192ed03+uint64(i)), 0644); err != nil {
			return err
		}
	}
	return nil
}

func compareBenchTrees(source, restored string) error {
	for _, root := range []string{source, restored} {
		if _, err := os.Stat(root); err != nil {
			return err
		}
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		a, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(filepath.Join(restored, rel))
		if err != nil {
			return err
		}
		if sha256.Sum256(a) != sha256.Sum256(b) {
			return fmt.Errorf("restored content differs: %s", rel)
		}
		return nil
	})
}

func writeBenchCSV(path string, runs []benchRun) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"trial", "dataset_bytes", "initial_backup_mib_per_second", "incremental_backup_mib_per_second", "incremental_chunk_reuse_percent", "verify_seconds", "restore_mib_per_second", "repository_bytes", "space_saved_percent", "go_allocated_bytes"}); err != nil {
		return err
	}
	for _, r := range runs {
		if err := w.Write([]string{strconv.Itoa(r.Trial), strconv.FormatInt(r.DatasetBytes, 10), strconv.FormatFloat(r.InitialBackupMiBPerSec, 'f', 3, 64), strconv.FormatFloat(r.IncrementalMiBPerSec, 'f', 3, 64), strconv.FormatFloat(r.IncrementalReusePct, 'f', 3, 64), strconv.FormatFloat(r.VerifySeconds, 'f', 3, 64), strconv.FormatFloat(r.RestoreMiBPerSec, 'f', 3, 64), strconv.FormatInt(r.RepositoryBytes, 10), strconv.FormatFloat(r.SpaceSavedPct, 'f', 3, 64), strconv.FormatUint(r.AllocatedBytes, 10)}); err != nil {
			return err
		}
	}
	return nil
}
