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
}
