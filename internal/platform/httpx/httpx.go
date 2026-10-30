// Package httpx contains HTTP plumbing shared by all modules: JSON encoding,
// strict decoding, RFC 9457 problem responses and the error-returning handler
// adapter. Business modules keep their handlers thin and return errors.
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"

	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/reqmeta"
)

// MaxJSONBody is the default request body limit for JSON endpoints.
const MaxJSONBody = 1 << 20 // 1 MiB

// HandlerFunc is an http handler that returns an error instead of writing it.
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// Problem is an RFC 9457 problem details body.
type Problem struct {
	Type      string              `json:"type"`
	Title     string              `json:"title"`
	Status    int                 `json:"status"`
	Code      string              `json:"code"`
	Detail    string              `json:"detail,omitempty"`
	RequestID string              `json:"request_id,omitempty"`
	Errors    []apperr.FieldError `json:"errors,omitempty"`
	Upstream  *apperr.Upstream    `json:"upstream,omitempty"`
}

// Adapter converts HandlerFuncs into http.Handlers with centralized error
// handling.
type Adapter struct {
	Log *slog.Logger
}

// H wraps fn.
func (a Adapter) H(fn HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			a.WriteError(w, r, err)
		}
	})
}

// WriteError writes err as a problem response. Internal causes are logged and
// never returned to the client.
func (a Adapter) WriteError(w http.ResponseWriter, r *http.Request, err error) {
	e, ok := apperr.As(err)
	if !ok {
		e = apperr.Internal(err)
	}
	status := statusFor(e.Kind)
	if status >= 500 {
		a.Log.ErrorContext(r.Context(), "request failed",
			slog.String("error", err.Error()),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path))
	}
	p := Problem{
		Type:      "about:blank",
		Title:     http.StatusText(status),
		Status:    status,
		Code:      e.Code,
		Detail:    e.Message,
		RequestID: reqmeta.From(r.Context()).RequestID,
		Errors:    e.Fields,
		Upstream:  e.Upstream,
	}
	if e.Kind == apperr.KindRateLimited && w.Header().Get("Retry-After") == "" {
		w.Header().Set("Retry-After", "60")
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}

func statusFor(k apperr.Kind) int {
	switch k {
	case apperr.KindBadRequest:
		return http.StatusBadRequest
	case apperr.KindUnauthenticated:
		return http.StatusUnauthorized
	case apperr.KindForbidden:
		return http.StatusForbidden
	case apperr.KindNotFound:
		return http.StatusNotFound
	case apperr.KindConflict:
		return http.StatusConflict
	case apperr.KindGone:
		return http.StatusGone
	case apperr.KindPreconditionFailed:
		return http.StatusPreconditionFailed
	case apperr.KindInvalid:
		return http.StatusUnprocessableEntity
	case apperr.KindRateLimited:
		return http.StatusTooManyRequests
	case apperr.KindUnsupportedMediaType:
		return http.StatusUnsupportedMediaType
	case apperr.KindPayloadTooLarge:
		return http.StatusRequestEntityTooLarge
	case apperr.KindFailedDependency:
		return http.StatusFailedDependency
	case apperr.KindUpstream:
		return http.StatusBadGateway
	case apperr.KindUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// Data is the success envelope for single resources.
type Data[T any] struct {
	Data T `json:"data"`
}

// List is the success envelope for collections.
type List[T any] struct {
	Data       []T     `json:"data"`
	NextCursor *string `json:"next_cursor"`
}

// JSON writes v with the given status.
func JSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

// NoContent writes 204.
func NoContent(w http.ResponseWriter) error {
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Decode strictly decodes a JSON body into dst: content type must be JSON,
// size is limited, unknown fields and trailing data are rejected.
func Decode(w http.ResponseWriter, r *http.Request, dst any) error {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		return &apperr.Error{Kind: apperr.KindUnsupportedMediaType, Code: "unsupported_media_type",
			Message: "Content-Type must be application/json."}
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooBig *http.MaxBytesError
		var syntax *json.SyntaxError
		var typ *json.UnmarshalTypeError
		switch {
		case errors.As(err, &tooBig):
			return &apperr.Error{Kind: apperr.KindPayloadTooLarge, Code: "payload_too_large",
				Message: "Request body is too large."}
		case errors.Is(err, io.EOF):
			return apperr.BadRequest("bad_request", "Request body is empty.")
		case errors.As(err, &syntax), errors.Is(err, io.ErrUnexpectedEOF):
			return apperr.BadRequest("bad_request", "Request body is not valid JSON.")
		case errors.As(err, &typ):
			return apperr.Invalid(apperr.FieldError{Field: typ.Field, Code: "invalid_type",
				Message: "Has the wrong type."})
		default:
			// Unknown field errors from encoding/json have no typed error.
			return apperr.BadRequest("bad_request", "Request body is invalid: "+safeJSONMsg(err))
		}
	}
	if dec.More() {
		return apperr.BadRequest("bad_request", "Request body must contain a single JSON object.")
	}
	return nil
}

// safeJSONMsg keeps only the "unknown field" message; other decoder messages
// may echo user input.
func safeJSONMsg(err error) string {
	const prefix = "json: unknown field "
	if s := err.Error(); len(s) > len(prefix) && s[:len(prefix)] == prefix {
		return "unknown field " + s[len(prefix):]
	}
	return "malformed body"
}

// QueryInt parses an optional integer query parameter within [min, max].
func QueryInt(r *http.Request, key string, def, min, max int) (int, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min || n > max {
		return 0, apperr.Invalid(apperr.FieldError{Field: key, Code: "out_of_range",
			Message: "Must be an integer between " + strconv.Itoa(min) + " and " + strconv.Itoa(max) + "."})
	}
	return n, nil
}
