// Package reqmeta carries per-request metadata (request ID, client IP, user
// agent) through context.Context so that lower layers such as audit logging
// can record it without depending on net/http.
package reqmeta

import (
	"context"
	"net/netip"
)

// Meta describes the origin of a request or job.
type Meta struct {
	RequestID string
	IP        *netip.Addr
	UserAgent string
}

type ctxKey struct{}

// With returns a context carrying m.
func With(ctx context.Context, m Meta) context.Context {
	return context.WithValue(ctx, ctxKey{}, m)
}

// From returns the metadata in ctx, or the zero value.
func From(ctx context.Context) Meta {
	m, _ := ctx.Value(ctxKey{}).(Meta)
	return m
}
