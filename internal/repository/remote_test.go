package repository

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/junaid/vestige/internal/chunker"
)

// memoryStore exercises the repository against object-store semantics without
// needing AWS credentials. The production S3 adapter is intentionally thin;
// this test protects the manifest-as-commit protocol used by every backend.
type memoryStore struct{ objects map[string][]byte }

// faultStore injects object-store failures for tests.
type faultStore struct {
	objectStore
	getErr error
	putErr error
}

func (s faultStore) Get(key string) ([]byte, string, error) {
	if s.getErr != nil {
		return nil, "", s.getErr
	}
	return s.objectStore.Get(key)
}

func (s faultStore) PutIfAbsent(key string, data []byte) (string, error) {
	if s.putErr != nil {
		return "", s.putErr
	}
	return s.objectStore.PutIfAbsent(key, data)
}

func (s *memoryStore) Get(key string) ([]byte, string, error) {
	b, ok := s.objects[key]
	if !ok {
		return nil, "", errObjectNotFound
	}
	copy := append([]byte(nil), b...)
	return copy, localVersion(copy), nil
}

func (s *memoryStore) PutIfAbsent(key string, data []byte) (string, error) {
	if _, exists := s.objects[key]; exists {
		return "", errObjectExists
	}
	s.objects[key] = append([]byte(nil), data...)
	return localVersion(data), nil
}

func (s *memoryStore) ReplaceIfVersion(key string, data []byte, version string) (string, error) {
	old, actual, err := s.Get(key)
	_ = old
	if err != nil {
		return "", err
	}
	if actual != version {
		return "", errObjectChanged
	}
	s.objects[key] = append([]byte(nil), data...)
	return localVersion(data), nil
}

func (s *memoryStore) DeleteIfVersion(key, version string) error {
	b, actual, err := s.Get(key)
	_ = b
	if err != nil {
		return err
	}
	if actual != version {
		return errObjectChanged
	}
	delete(s.objects, key)
	return nil
}

func (s *memoryStore) Delete(key string) error {
	if _, ok := s.objects[key]; !ok {
		return errObjectNotFound
	}
	delete(s.objects, key)
	return nil
}

func (s *memoryStore) List(prefix string) ([]storedObject, error) {
	var out []storedObject
	for key, data := range s.objects {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			out = append(out, storedObject{Key: key, Size: int64(len(data)), Version: localVersion(data)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func openRemoteTestRepository(t *testing.T, store objectStore) *Repository {
	t.Helper()
	r := &Repository{Root: "s3://test-bucket/vestige", store: store, remote: true}
	if err := r.ensureConfig(RepositoryOptions{Compression: "none"}); err != nil {
		t.Fatal(err)
	}
	if err := r.setupCodec(); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRemoteRepositoryBackupRestoreAndDelete(t *testing.T) {
	store := &memoryStore{objects: map[string][]byte{}}
	r := openRemoteTestRepository(t, store)
	source := filepath.Join(t.TempDir(), "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	want := bytes.Repeat([]byte("remote object storage\n"), 20)
	if err := os.WriteFile(filepath.Join(source, "file.txt"), want, 0644); err != nil {
		t.Fatal(err)
	}

	m, stats, err := r.Backup(source, chunker.Config{Window: 8, MinSize: 4, TargetSize: 16, MaxSize: 64})
	if err != nil {
		t.Fatal(err)
	}
	if stats.ChunksCreated == 0 {
		t.Fatal("backup did not upload chunks")
	}
	if _, ok := store.objects[r.manifestKey(m.SnapshotID)]; !ok {
		t.Fatal("manifest commit object missing")
	}
	if _, ok := store.objects["write.lock"]; ok {
		t.Fatal("writer lock was not released")
	}

	reopened := openRemoteTestRepository(t, store)
	destination := filepath.Join(t.TempDir(), "restore")
	if err := reopened.Restore(m.SnapshotID, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("restored remote file differs")
	}
	if err := reopened.Verify(m.SnapshotID); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.DeleteSnapshot(m.SnapshotID, false, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ReadManifest(m.SnapshotID); !errors.Is(err, errObjectNotFound) {
		t.Fatalf("deleted manifest error = %v, want not found", err)
	}
}

func TestObjectStoreFaultsDoNotPublishPartialSnapshots(t *testing.T) {
	backing := &memoryStore{objects: map[string][]byte{}}
	faults := &faultStore{objectStore: backing}
	r := openRemoteTestRepository(t, faults)
	source := filepath.Join(t.TempDir(), "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte("network and disk fault fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	// A failed write covers both a connection loss during upload and a local
	// storage-full error: neither may publish a manifest commit record.
	faults.putErr = errors.New("injected upload failure")
	if _, _, err := r.Backup(source, chunker.DefaultConfig()); err == nil {
		t.Fatal("backup unexpectedly survived a failed chunk write")
	}
	if snapshots, err := r.Snapshots(); err != nil || len(snapshots) != 0 {
		t.Fatalf("failed backup published snapshots: %v, %v", snapshots, err)
	}
	faults.putErr = nil
	snapshot, _, err := r.Backup(source, chunker.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	// A read-side network loss must be surfaced rather than treated as a clean
	// verification result.
	faults.getErr = errors.New("injected connection reset")
	if err := r.Verify(snapshot.SnapshotID); err == nil {
		t.Fatal("verify unexpectedly accepted a failed object read")
	}
}

func TestS3StoreUsesConditionalObjectWrites(t *testing.T) {
	objects := map[string][]byte{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path[len("/bucket/"):]
		switch r.Method {
		case http.MethodPut:
			if r.Header.Get("If-None-Match") == "*" {
				if _, exists := objects[key]; exists {
					w.WriteHeader(http.StatusPreconditionFailed)
					_, _ = io.WriteString(w, `<Error><Code>PreconditionFailed</Code></Error>`)
					return
				}
			}
			body, _ := io.ReadAll(r.Body)
			objects[key] = body
			w.Header().Set("ETag", `"test-version"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			body, ok := objects[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("ETag", `"test-version"`)
			_, _ = w.Write(body)
		case http.MethodDelete:
			if r.Header.Get("If-Match") != `"test-version"` {
				w.WriteHeader(http.StatusPreconditionFailed)
				return
			}
			delete(objects, key)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	cfg := aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test", "test", "")}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) { o.BaseEndpoint = aws.String(server.URL); o.UsePathStyle = true })
	store := &s3Store{client: client, bucket: "bucket", prefix: "repo"}
	version, err := store.PutIfAbsent("config.json", []byte("config"))
	if err != nil {
		t.Fatal(err)
	}
	if version != `"test-version"` {
		t.Fatalf("version = %q", version)
	}
	if _, err := store.PutIfAbsent("config.json", []byte("again")); !errors.Is(err, errObjectExists) {
		t.Fatalf("second put error = %v", err)
	}
	data, gotVersion, err := store.Get("config.json")
	if err != nil || !bytes.Equal(data, []byte("config")) || gotVersion != version {
		t.Fatalf("get = %q, %q, %v", data, gotVersion, err)
	}
	if err := store.DeleteIfVersion("config.json", version); err != nil {
		t.Fatal(err)
	}
}
