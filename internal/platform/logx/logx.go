// Package logx configures structured JSON logging with secret redaction.
package logx

import (
	"io"
	"log/slog"
	"strings"
)

// Redacted is the replacement value for sensitive attributes.
const Redacted = "[REDACTED]"

// sensitiveKeys are attribute keys (lowercased) whose values are never logged.
var sensitiveKeys = map[string]struct{}{
	"password":      {},
	"new_password":  {},
	"password_hash": {},
	"token":         {},
	"access_token":  {},
	"refresh_token": {},
	"partner_key":   {},
	"sign":          {},
	"authorization": {},
	"cookie":        {},
	"set-cookie":    {},
	"csrf_token":    {},
	"secret":        {},
	"api_key":       {},
	"code":          {}, // Shopee OAuth authorization code
}

// sensitiveSuffixes catch variants such as "device_token" or "client_secret".
var sensitiveSuffixes = []string{"_token", "_secret", "_password", "_key"}

// IsSensitiveKey reports whether values under key must be redacted.
func IsSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	if _, ok := sensitiveKeys[k]; ok {
		return true
	}
	for _, s := range sensitiveSuffixes {
		if strings.HasSuffix(k, s) {
			return true
		}
	}
	return false
}

// New returns a JSON logger that redacts sensitive attributes at any depth.
func New(w io.Writer, level slog.Leveler) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if IsSensitiveKey(a.Key) {
				return slog.String(a.Key, Redacted)
			}
			return a
		},
	})
	return slog.New(h)
}

// RedactMap returns a copy of m with sensitive keys replaced, recursively.
// Use it before persisting free-form metadata (audit logs, events).
func RedactMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if IsSensitiveKey(k) {
			out[k] = Redacted
			continue
		}
		switch t := v.(type) {
		case map[string]any:
			out[k] = RedactMap(t)
		case []any:
			items := make([]any, len(t))
			for i, it := range t {
				if mm, ok := it.(map[string]any); ok {
					items[i] = RedactMap(mm)
				} else {
					items[i] = it
				}
			}
			out[k] = items
		default:
			out[k] = v
		}
	}
	return out
}
