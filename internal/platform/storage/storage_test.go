package storage

import (
	"context"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestS3 runs against the development S3 (make up) when TEST_S3_ENDPOINT is set.
func TestS3(t *testing.T) {
	ep := os.Getenv("TEST_S3_ENDPOINT")
	if ep == "" {
		if os.Getenv("TEST_REQUIRE_DB") == "1" {
			t.Fatal("TEST_S3_ENDPOINT is required")
		}
		t.Skip("set TEST_S3_ENDPOINT")
	}
	ctx := context.Background()
	s, err := NewS3(ctx, Config{Endpoint: ep, AccessKey: os.Getenv("TEST_S3_ACCESS_KEY"), SecretKey: os.Getenv("TEST_S3_SECRET_KEY"),
		Bucket: "test-" + time.Now().Format("20060102"), Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	key := "t/" + time.Now().Format(time.RFC3339Nano) + ".txt"
	if err := s.Put(ctx, key, []byte("hello"), "text/plain"); err != nil {
		t.Fatal(err)
	}
	b, err := s.Get(ctx, key, 10)
	if err != nil || string(b) != "hello" {
		t.Fatalf("get: %q %v", b, err)
	}
	if _, err := s.Get(ctx, key, 2); err == nil {
		t.Fatal("size limit ignored")
	}
	u, err := s.PresignGet(ctx, key, time.Minute, "report.csv")
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Get(u) // #nosec G107 -- test against local S3
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || string(body) != "hello" {
		t.Fatalf("presigned: %d %q", res.StatusCode, body)
	}

	// DeletePrefix removes only the objects under the directory.
	dir := "purge-" + time.Now().Format("150405.000000") + "/"
	for _, k := range []string{dir + "a.txt", dir + "sub/b.txt", "keep-" + dir + "c.txt"} {
		if err := s.Put(ctx, k, []byte("x"), "text/plain"); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.DeletePrefix(ctx, dir); err != nil || n != 2 {
		t.Fatalf("delete prefix: %d %v", n, err)
	}
	if _, err := s.Get(ctx, dir+"a.txt", 10); err == nil {
		t.Fatal("object not deleted")
	}
	if _, err := s.Get(ctx, "keep-"+dir+"c.txt", 10); err != nil {
		t.Fatalf("other object deleted: %v", err)
	}
	if _, err := s.DeletePrefix(ctx, "no-slash"); err == nil {
		t.Fatal("prefix without slash accepted")
	}
}
