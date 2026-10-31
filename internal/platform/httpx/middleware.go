package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/reqmeta"
)

var requestIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]{8,64}$`)

// RequestMeta assigns a request ID (accepting a well-formed incoming
// X-Request-ID), resolves the client IP and stores both in the context.
func RequestMeta(trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if !requestIDRe.MatchString(id) {
				b := make([]byte, 12)
				_, _ = rand.Read(b)
				id = hex.EncodeToString(b)
			}
			w.Header().Set("X-Request-ID", id)
			m := reqmeta.Meta{RequestID: id, IP: clientIP(r, trustProxy), UserAgent: truncate(r.UserAgent(), 512)}
			next.ServeHTTP(w, r.WithContext(reqmeta.With(r.Context(), m)))
		})
	}
}

func clientIP(r *http.Request, trustProxy bool) *netip.Addr {
	if trustProxy {
		// Nginx sets X-Forwarded-For; the left-most entry is the client.
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first := strings.TrimSpace(strings.Split(xff, ",")[0])
			if a, err := netip.ParseAddr(first); err == nil {
				return &a
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if a, err := netip.ParseAddr(host); err == nil {
		a = a.Unmap()
		return &a
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Recoverer turns panics into 500 responses and logs the stack.
func Recoverer(a Adapter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if p := recover(); p != nil {
					if err, ok := p.(error); ok && errors.Is(err, http.ErrAbortHandler) {
						panic(p)
					}
					a.Log.ErrorContext(r.Context(), "panic", slog.Any("panic", p), slog.String("stack", string(debug.Stack())))
					a.WriteError(w, r, apperr.Internal(nil))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// AccessLog logs one line per request. Query strings are not logged because
// they may contain tokens (e.g. OAuth callbacks).
func AccessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			m := reqmeta.From(r.Context())
			ip := ""
			if m.IP != nil {
				ip = m.IP.String()
			}
			log.InfoContext(r.Context(), "http request",
				slog.String("request_id", m.RequestID),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int("bytes", rec.bytes),
				slog.Duration("duration", time.Since(start)),
				slog.String("ip", ip))
		})
	}
}

// SecurityHeaders sets conservative defaults for an API that serves JSON.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// SameOrigin rejects unsafe requests whose Origin (or, if absent, Referer)
// does not match allowedOrigin. Browsers always send Origin on cross-site
// POSTs, so this blocks login CSRF and complements the CSRF token.
func SameOrigin(a Adapter, allowedOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isSafeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			origin := r.Header.Get("Origin")
			if origin == "" {
				if ref := r.Header.Get("Referer"); ref != "" {
					origin = originOf(ref)
				}
			}
			if origin != "" && origin != allowedOrigin {
				a.WriteError(w, r, apperr.Forbidden("csrf_failed", "Yêu cầu từ nguồn khác đã bị chặn. Hãy tải lại trang và thử lại."))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func originOf(u string) string {
	i := strings.Index(u, "://")
	if i < 0 {
		return ""
	}
	rest := u[i+3:]
	if j := strings.IndexAny(rest, "/?#"); j >= 0 {
		rest = rest[:j]
	}
	return u[:i+3] + rest
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// IsSafeMethod reports whether m is a read-only HTTP method.
func IsSafeMethod(m string) bool { return isSafeMethod(m) }
