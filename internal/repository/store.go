package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

var (
	errObjectNotFound = errors.New("object not found")
	errObjectExists   = errors.New("object already exists")
	errObjectChanged  = errors.New("object changed")
)

// objectStore is the small, durable object API used by a repository. Keys are
// slash-separated and relative to one repository. PutIfAbsent is the commit
// primitive used for chunks, manifests, and writer locks.
type objectStore interface {
	Get(key string) (data []byte, version string, err error)
	PutIfAbsent(key string, data []byte) (version string, err error)
	DeleteIfVersion(key, version string) error
	Delete(key string) error
	List(prefix string) ([]storedObject, error)
}

type storedObject struct {
	Key     string
	Size    int64
	Version string
}

type localStore struct{ root string }

func (s localStore) path(key string) (string, error) {
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, `\`) {
		return "", fmt.Errorf("invalid repository object key %q", key)
	}
	for _, part := range strings.Split(key, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("invalid repository object key %q", key)
		}
	}
	return filepath.Join(s.root, filepath.FromSlash(key)), nil
}

func localVersion(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s localStore) Get(key string) ([]byte, string, error) {
	path, err := s.path(key)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", errObjectNotFound
	}
	if err != nil {
		return nil, "", err
	}
	return data, localVersion(data), nil
}

func (s localStore) PutIfAbsent(key string, data []byte) (string, error) {
	path, err := s.path(key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".vestige-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	// Link creation is atomic and never replaces an existing target. It retains
	// the old temp-and-sync durability property without an overwrite race.
	if err := os.Link(tmpName, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", errObjectExists
		}
		return "", err
	}
	return localVersion(data), nil
}

func (s localStore) DeleteIfVersion(key, version string) error {
	data, actual, err := s.Get(key)
	_ = data
	if err != nil {
		return err
	}
	if actual != version {
		return errObjectChanged
	}
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); errors.Is(err, os.ErrNotExist) {
		return errObjectChanged
	} else {
		return err
	}
}

func (s localStore) Delete(key string) error {
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); errors.Is(err, os.ErrNotExist) {
		return errObjectNotFound
	} else {
		return err
	}
}

func (s localStore) List(prefix string) ([]storedObject, error) {
	var objects []storedObject
	err := filepath.WalkDir(s.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if !strings.HasPrefix(key, prefix) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		objects = append(objects, storedObject{Key: key, Size: info.Size()})
		return nil
	})
	return objects, err
}

type s3Store struct {
	client *s3.Client
	bucket string
	prefix string
}

func newS3Store(raw string, options RepositoryOptions) (*s3Store, string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "s3" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, "", fmt.Errorf("invalid S3 repository URL %q (use s3://bucket/optional-prefix)", raw)
	}
	prefix := strings.Trim(u.EscapedPath(), "/")
	if prefix != "" {
		for _, part := range strings.Split(prefix, "/") {
			if part == "" || part == "." || part == ".." {
				return nil, "", fmt.Errorf("invalid S3 repository prefix %q", u.Path)
			}
		}
	}
	region := options.S3Region
	if region == "" {
		region = os.Getenv("VESTIGE_S3_REGION")
	}
	load := []func(*awsconfig.LoadOptions) error{}
	if region != "" {
		load = append(load, awsconfig.WithRegion(region))
	}
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(), load...)
	if err != nil {
		return nil, "", fmt.Errorf("load AWS configuration: %w", err)
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	endpoint := options.S3Endpoint
	if endpoint == "" {
		endpoint = os.Getenv("VESTIGE_S3_ENDPOINT")
	}
	pathStyle := options.S3PathStyle || strings.EqualFold(os.Getenv("VESTIGE_S3_PATH_STYLE"), "true")
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		o.UsePathStyle = pathStyle
	})
	root := "s3://" + u.Host
	if prefix != "" {
		root += "/" + prefix
	}
	return &s3Store{client: client, bucket: u.Host, prefix: prefix}, root, nil
}

func (s *s3Store) fullKey(key string) string {
	if s.prefix == "" {
		return key
	}
	return s.prefix + "/" + key
}

func s3KeyError(err error) error {
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "nosuchkey") || strings.Contains(text, "notfound") || strings.Contains(text, "status code: 404") {
		return errObjectNotFound
	}
	if strings.Contains(text, "preconditionfailed") || strings.Contains(text, "conditionalrequestconflict") || strings.Contains(text, "status code: 412") || strings.Contains(text, "status code: 409") {
		return errObjectExists
	}
	return err
}

func (s *s3Store) Get(key string) ([]byte, string, error) {
	out, err := s.client.GetObject(context.Background(), &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.fullKey(key))})
	if err != nil {
		return nil, "", s3KeyError(err)
	}
	defer out.Body.Close()
	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, "", err
	}
	return data, aws.ToString(out.ETag), nil
}

func (s *s3Store) PutIfAbsent(key string, data []byte) (string, error) {
	out, err := s.client.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.fullKey(key)), Body: bytes.NewReader(data), IfNoneMatch: aws.String("*")})
	if err != nil {
		return "", s3KeyError(err)
	}
	return aws.ToString(out.ETag), nil
}

func (s *s3Store) DeleteIfVersion(key, version string) error {
	_, err := s.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.fullKey(key)), IfMatch: aws.String(version)})
	if err != nil {
		if mapped := s3KeyError(err); errors.Is(mapped, errObjectExists) {
			return errObjectChanged
		} else {
			return mapped
		}
	}
	return nil
}

func (s *s3Store) Delete(key string) error {
	_, err := s.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.fullKey(key))})
	if err == nil {
		return nil
	}
	return s3KeyError(err)
}

func (s *s3Store) List(prefix string) ([]storedObject, error) {
	var objects []storedObject
	prefix = s.fullKey(prefix)
	pager := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket), Prefix: aws.String(prefix)})
	for pager.HasMorePages() {
		page, err := pager.NextPage(context.Background())
		if err != nil {
			return nil, s3KeyError(err)
		}
		for _, object := range page.Contents {
			key := strings.TrimPrefix(aws.ToString(object.Key), s.prefix)
			key = strings.TrimPrefix(key, "/")
			objects = append(objects, storedObject{Key: key, Size: aws.ToInt64(object.Size), Version: aws.ToString(object.ETag)})
		}
	}
	return objects, nil
}
