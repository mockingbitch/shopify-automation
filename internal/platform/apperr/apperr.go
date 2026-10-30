// Package apperr defines transport-independent application errors. Services
// return these; only the HTTP layer maps them to status codes.
package apperr

import (
	"errors"
	"fmt"
)

// Kind classifies an error for the transport layer.
type Kind int

const (
	KindInternal Kind = iota
	KindBadRequest
	KindUnauthenticated
	KindForbidden
	KindNotFound
	KindConflict
	KindGone
	KindPreconditionFailed
	KindInvalid
	KindRateLimited
	KindUnsupportedMediaType
	KindPayloadTooLarge
	// KindFailedDependency: the operation needs something that is not in a
	// usable state, e.g. a Shopee connection that must be reconnected (424).
	KindFailedDependency
	// KindUpstream: an upstream service (Shopee) failed or rejected the call (502).
	KindUpstream
	// KindUnavailable: a dependency is temporarily unavailable (503).
	KindUnavailable
)

// Upstream describes a normalized upstream failure. Handlers attach it only
// for callers allowed to see integration details.
type Upstream struct {
	Code      string `json:"code"`
	Message   string `json:"message,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// FieldError describes one invalid input field.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error is an application error with a stable machine code and a message that
// is safe to show to users.
type Error struct {
	Kind    Kind
	Code    string
	Message string
	Fields  []FieldError
	// Upstream is shown to the client when set; see Upstream.
	Upstream *Upstream
	Err      error // internal cause, never shown to users
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return e.Code + ": " + e.Message
}

func (e *Error) Unwrap() error { return e.Err }

// As extracts an *Error from err.
func As(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}

// IsKind reports whether err is an application error of kind k.
func IsKind(err error, k Kind) bool {
	e, ok := As(err)
	return ok && e.Kind == k
}

func newErr(k Kind, code, msg string) *Error { return &Error{Kind: k, Code: code, Message: msg} }

func BadRequest(code, msg string) *Error { return newErr(KindBadRequest, code, msg) }
func Unauthenticated(msg string) *Error  { return newErr(KindUnauthenticated, "unauthenticated", msg) }
func Forbidden(code, msg string) *Error  { return newErr(KindForbidden, code, msg) }
func NotFound(msg string) *Error         { return newErr(KindNotFound, "not_found", msg) }
func Conflict(code, msg string) *Error   { return newErr(KindConflict, code, msg) }
func Gone(code, msg string) *Error       { return newErr(KindGone, code, msg) }
func RateLimited(msg string) *Error      { return newErr(KindRateLimited, "rate_limited", msg) }
func PreconditionFailed(msg string) *Error {
	return newErr(KindPreconditionFailed, "precondition_failed", msg)
}

// FailedDependency returns a 424 error.
func FailedDependency(code, msg string) *Error { return newErr(KindFailedDependency, code, msg) }

// Unavailable returns a 503 error.
func Unavailable(code, msg string) *Error { return newErr(KindUnavailable, code, msg) }

// UpstreamFailed returns a 502 error.
func UpstreamFailed(code, msg string) *Error { return newErr(KindUpstream, code, msg) }

// Invalid returns a validation error carrying field errors.
func Invalid(fields ...FieldError) *Error {
	e := newErr(KindInvalid, "validation_failed", "Some fields are invalid.")
	e.Fields = fields
	return e
}

// Internal wraps an unexpected error. The cause is logged, never returned.
func Internal(err error) *Error {
	return &Error{Kind: KindInternal, Code: "internal", Message: "An unexpected error occurred.", Err: err}
}
