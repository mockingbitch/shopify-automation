// Package storage stores files in S3-compatible object storage. Objects are
// private; clients get short-lived presigned URLs.
package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ErrNotFound is returned for a missing object.
var ErrNotFound = errors.New("storage: object not found")

// Store is what the application needs from object storage.
type Store interface {
	Put(ctx context.Context, key string, body []byte, contentType string) error
	Get(ctx context.Context, key string, maxBytes int64) ([]byte, error)
	PresignGet(ctx context.Context, key string, ttl time.Duration, filename string) (string, error)
	// DeletePrefix removes every object under prefix and returns how many.
	DeletePrefix(ctx context.Context, prefix string) (int, error)
}

// Config locates the bucket.
type Config struct {
	Endpoint  string // host:port
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string
	UseSSL    bool
}

// S3 is the production Store.
type S3 struct {
	client *minio.Client
	bucket string
}

// NewS3 connects and creates the bucket if missing.
func NewS3(ctx context.Context, c Config) (*S3, error) {
	cl, err := minio.New(c.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(c.AccessKey, c.SecretKey, ""),
		Secure: c.UseSSL,
		Region: c.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: client: %w", err)
	}
	ok, err := cl.BucketExists(ctx, c.Bucket)
	if err != nil {
		return nil, fmt.Errorf("storage: bucket check: %w", err)
	}
	if !ok {
		if err := cl.MakeBucket(ctx, c.Bucket, minio.MakeBucketOptions{Region: c.Region}); err != nil {
			return nil, fmt.Errorf("storage: create bucket: %w", err)
		}
	}
	return &S3{client: cl, bucket: c.Bucket}, nil
}

// Put stores body under key.
func (s *S3) Put(ctx context.Context, key string, body []byte, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(body), int64(len(body)),
		minio.PutObjectOptions{ContentType: contentType, ServerSideEncryption: nil})
	if err != nil {
		return fmt.Errorf("storage: put %s: %w", key, err)
	}
	return nil
}

// Get reads an object, refusing objects larger than maxBytes.
func (s *S3) Get(ctx context.Context, key string, maxBytes int64) ([]byte, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("storage: get %s: %w", key, err)
	}
	defer func() { _ = obj.Close() }()
	b, err := io.ReadAll(io.LimitReader(obj, maxBytes+1))
	if err != nil {
		var me minio.ErrorResponse
		if errors.As(err, &me) && me.Code == "NoSuchKey" {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: read %s: %w", key, err)
	}
	if int64(len(b)) > maxBytes {
		return nil, fmt.Errorf("storage: %s is larger than %d bytes", key, maxBytes)
	}
	return b, nil
}

// DeletePrefix implements Store.
func (s *S3) DeletePrefix(ctx context.Context, prefix string) (int, error) {
	if prefix == "" || !strings.HasSuffix(prefix, "/") {
		return 0, errors.New("storage: refusing to delete without a directory prefix")
	}
	objects := make(chan minio.ObjectInfo)
	n := 0
	var listErr error
	go func() {
		defer close(objects)
		for o := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
			if o.Err != nil {
				listErr = o.Err
				return
			}
			n++
			objects <- o
		}
	}()
	for e := range s.client.RemoveObjects(ctx, s.bucket, objects, minio.RemoveObjectsOptions{}) {
		if e.Err != nil {
			return n, fmt.Errorf("storage: delete %s: %w", e.ObjectName, e.Err)
		}
	}
	return n, listErr
}

// PresignGet returns a temporary download URL.
func (s *S3) PresignGet(ctx context.Context, key string, ttl time.Duration, filename string) (string, error) {
	params := url.Values{}
	if filename != "" {
		params.Set("response-content-disposition", `attachment; filename="`+filename+`"`)
	}
	u, err := s.client.PresignedGetObject(ctx, s.bucket, key, ttl, params)
	if err != nil {
		return "", fmt.Errorf("storage: presign %s: %w", key, err)
	}
	return u.String(), nil
}

// Memory is an in-process Store for tests.
type Memory struct {
	mu       sync.Mutex
	objects  map[string][]byte
	failGets int
}

// FailNextGets makes the next n Get calls fail, like an unavailable bucket.
func (m *Memory) FailNextGets(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failGets = n
}

// Put implements Store.
func (m *Memory) Put(_ context.Context, key string, body []byte, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.objects == nil {
		m.objects = map[string][]byte{}
	}
	m.objects[key] = append([]byte(nil), body...)
	return nil
}

// Get implements Store.
func (m *Memory) Get(_ context.Context, key string, maxBytes int64) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failGets > 0 {
		m.failGets--
		return nil, errors.New("storage: unavailable")
	}
	b, ok := m.objects[key]
	if !ok {
		return nil, ErrNotFound
	}
	if int64(len(b)) > maxBytes {
		return nil, fmt.Errorf("storage: %s is larger than %d bytes", key, maxBytes)
	}
	return append([]byte(nil), b...), nil
}

// DeletePrefix implements Store.
func (m *Memory) DeletePrefix(_ context.Context, prefix string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for k := range m.objects {
		if strings.HasPrefix(k, prefix) {
			delete(m.objects, k)
			n++
		}
	}
	return n, nil
}

// Keys returns the stored keys with a prefix (tests).
func (m *Memory) Keys(prefix string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for k := range m.objects {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out
}

// PresignGet implements Store with a fake URL.
func (m *Memory) PresignGet(_ context.Context, key string, _ time.Duration, _ string) (string, error) {
	return "https://storage.test/" + url.PathEscape(key), nil
}
