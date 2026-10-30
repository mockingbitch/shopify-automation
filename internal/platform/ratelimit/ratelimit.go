// Package ratelimit implements fixed-window request limits backed by Redis,
// with an in-memory implementation for tests and single-process tools.
package ratelimit

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter decides whether an action identified by key may proceed.
type Limiter interface {
	// Allow counts one hit for key in the current window. It returns whether
	// the hit is within limit and, if not, how long until the window resets.
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error)
}

// Redis is a Limiter using INCR + PEXPIRE on a per-window key.
type Redis struct {
	Client *redis.Client
	Prefix string
}

var incrScript = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
return {n, redis.call('PTTL', KEYS[1])}
`)

func (l Redis) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	res, err := incrScript.Run(ctx, l.Client, []string{l.Prefix + key}, window.Milliseconds()).Int64Slice()
	if err != nil {
		return true, 0, err // callers decide whether to fail open
	}
	if res[0] > int64(limit) {
		return false, time.Duration(res[1]) * time.Millisecond, nil
	}
	return true, 0, nil
}

// Memory is an in-process Limiter.
type Memory struct {
	mu      sync.Mutex
	windows map[string]memWindow
	Now     func() time.Time
}

type memWindow struct {
	count int
	reset time.Time
}

func (l *Memory) Allow(_ context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	now := time.Now()
	if l.Now != nil {
		now = l.Now()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.windows == nil {
		l.windows = map[string]memWindow{}
	}
	w := l.windows[key]
	if !now.Before(w.reset) {
		w = memWindow{reset: now.Add(window)}
	}
	w.count++
	l.windows[key] = w
	if w.count > limit {
		return false, w.reset.Sub(now), nil
	}
	return true, 0, nil
}
