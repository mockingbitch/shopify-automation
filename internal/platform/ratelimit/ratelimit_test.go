package ratelimit

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestMemoryLimiter(t *testing.T) {
	now := time.Unix(1000, 0)
	l := &Memory{Now: func() time.Time { return now }}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if ok, _, _ := l.Allow(ctx, "k", 3, time.Minute); !ok {
			t.Fatalf("hit %d should pass", i+1)
		}
	}
	ok, retry, _ := l.Allow(ctx, "k", 3, time.Minute)
	if ok || retry != time.Minute {
		t.Fatalf("4th hit: ok=%v retry=%v", ok, retry)
	}
	if ok, _, _ := l.Allow(ctx, "other", 3, time.Minute); !ok {
		t.Fatal("keys must be independent")
	}
	now = now.Add(time.Minute)
	if ok, _, _ := l.Allow(ctx, "k", 3, time.Minute); !ok {
		t.Fatal("window should reset")
	}
}

func TestRedisLimiter(t *testing.T) {
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("set TEST_REDIS_URL")
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	c := redis.NewClient(opt)
	defer func() { _ = c.Close() }()
	l := Redis{Client: c, Prefix: "test:" + t.Name() + ":" + time.Now().String() + ":"}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if ok, _, err := l.Allow(ctx, "k", 2, time.Minute); err != nil || !ok {
			t.Fatalf("hit %d: ok=%v err=%v", i+1, ok, err)
		}
	}
	ok, retry, err := l.Allow(ctx, "k", 2, time.Minute)
	if err != nil || ok || retry <= 0 || retry > time.Minute {
		t.Fatalf("3rd hit: ok=%v retry=%v err=%v", ok, retry, err)
	}
}
