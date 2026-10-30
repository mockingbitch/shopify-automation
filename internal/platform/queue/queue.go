// Package queue wraps background task delivery. Tasks carry only IDs; the
// handler reloads state from the database and re-checks the tenant, so a task
// in Redis is never trusted for authorization and never holds secrets.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hibiken/asynq"
)

// Task types.
const (
	TypeShopSync        = "shop:sync"
	TypeTokenRefresh    = "shopee:token_refresh"     // #nosec G101 -- task name, not a credential
	TypeTokenRefreshDue = "shopee:token_refresh_due" // #nosec G101 -- task name, not a credential
	TypeWebhookProcess  = "webhook:process"
	TypeWebhookSweep    = "webhook:sweep"
)

// Options controls one enqueue.
type Options struct {
	// ID deduplicates: a second task with the same ID is dropped while the
	// first is pending or retained.
	ID       string
	MaxRetry int
	Timeout  time.Duration
}

// Enqueuer schedules tasks.
type Enqueuer interface {
	Enqueue(ctx context.Context, typ string, payload any, opt Options) (string, error)
}

// Handler processes one task payload.
type Handler func(ctx context.Context, payload []byte) error

// Asynq is the production Enqueuer.
type Asynq struct {
	Client *asynq.Client
}

// Enqueue implements Enqueuer. A duplicate task ID is not an error.
func (q Asynq) Enqueue(ctx context.Context, typ string, payload any, opt Options) (string, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("queue: encode %s: %w", typ, err)
	}
	var opts []asynq.Option
	if opt.ID != "" {
		opts = append(opts, asynq.TaskID(opt.ID))
	}
	if opt.MaxRetry > 0 {
		opts = append(opts, asynq.MaxRetry(opt.MaxRetry))
	}
	if opt.Timeout > 0 {
		opts = append(opts, asynq.Timeout(opt.Timeout))
	}
	info, err := q.Client.EnqueueContext(ctx, asynq.NewTask(typ, b), opts...)
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return opt.ID, nil
	}
	if err != nil {
		return "", fmt.Errorf("queue: enqueue %s: %w", typ, err)
	}
	return info.ID, nil
}

// Mux registers handlers by task type and adapts them to asynq.
type Mux struct {
	Log      *slog.Logger
	handlers map[string]Handler
}

// Handle registers h for typ.
func (m *Mux) Handle(typ string, h Handler) {
	if m.handlers == nil {
		m.handlers = map[string]Handler{}
	}
	m.handlers[typ] = h
}

// Handlers returns the registered handlers (for Inline).
func (m *Mux) Handlers() map[string]Handler { return m.handlers }

// ServeMux builds the asynq mux.
func (m *Mux) ServeMux() *asynq.ServeMux {
	mux := asynq.NewServeMux()
	for typ, h := range m.handlers {
		mux.HandleFunc(typ, func(ctx context.Context, t *asynq.Task) error {
			start := time.Now()
			err := h(ctx, t.Payload())
			attrs := []any{slog.String("task", typ), slog.Duration("duration", time.Since(start))}
			if err != nil {
				m.Log.WarnContext(ctx, "task failed", append(attrs, slog.String("error", err.Error()))...)
				return err
			}
			m.Log.InfoContext(ctx, "task done", attrs...)
			return nil
		})
	}
	return mux
}

// Inline runs tasks synchronously in the calling goroutine. It is meant for
// tests and single-process tools; handlers are set after construction to
// break the service <-> queue cycle.
type Inline struct {
	mu       sync.Mutex
	Handlers map[string]Handler
	// Seen counts enqueued tasks by type (including deduplicated ones).
	Seen map[string]int
	// Errors records handler failures; tests can assert it is empty.
	Errors []error
	ids    map[string]bool
}

// Enqueue implements Enqueuer.
func (q *Inline) Enqueue(ctx context.Context, typ string, payload any, opt Options) (string, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	q.mu.Lock()
	if q.Seen == nil {
		q.Seen = map[string]int{}
		q.ids = map[string]bool{}
	}
	q.Seen[typ]++
	if opt.ID != "" && q.ids[opt.ID] {
		q.mu.Unlock()
		return opt.ID, nil
	}
	if opt.ID != "" {
		q.ids[opt.ID] = true
	}
	h := q.Handlers[typ]
	q.mu.Unlock()
	if h != nil {
		if err := h(context.WithoutCancel(ctx), b); err != nil {
			q.mu.Lock()
			q.Errors = append(q.Errors, fmt.Errorf("%s: %w", typ, err))
			q.mu.Unlock()
		}
	}
	id := opt.ID
	if id == "" {
		id = "inline"
	}
	return id, nil
}

// Count returns how many tasks of typ were enqueued.
func (q *Inline) Count(typ string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.Seen[typ]
}

// ResetIDs forgets deduplication IDs (tests that enqueue the same ID twice).
func (q *Inline) ResetIDs() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ids = map[string]bool{}
}

// Decode unmarshals a payload strictly.
func Decode(payload []byte, v any) error {
	if err := json.Unmarshal(payload, v); err != nil {
		return fmt.Errorf("%w: %w", asynq.SkipRetry, err)
	}
	return nil
}
