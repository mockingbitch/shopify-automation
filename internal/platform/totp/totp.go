// Package totp implements RFC 6238 time-based one-time passwords (SHA-1,
// 6 digits, 30-second steps), compatible with common authenticator apps.
package totp

import (
	"crypto/hmac"
	"crypto/sha1" // #nosec G505 -- RFC 6238 / authenticator apps use HMAC-SHA1; HMAC-SHA1 is not affected by SHA-1 collisions
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"

	"shopee-automation/internal/platform/securetoken"
)

const (
	// Period is the step length.
	Period = 30 * time.Second
	digits = 6
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewSecret returns a random 160-bit secret.
func NewSecret() []byte { return securetoken.Bytes(20) }

// Encode returns the base32 form users type into an authenticator app.
func Encode(secret []byte) string { return b32.EncodeToString(secret) }

// URI returns the otpauth:// URI for QR codes.
func URI(secret []byte, issuer, account string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{"secret": {Encode(secret)}, "issuer": {issuer}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// Step returns the step number of t.
func Step(t time.Time) int64 { return t.Unix() / int64(Period/time.Second) }

// Code returns the code of a step.
func Code(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step)) // #nosec G115 -- steps are positive
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1_000_000)
}

// Verify checks code against the steps t-1, t and t+1 (clock drift) and
// returns the matching step. Steps at or before lastUsed are rejected, so
// a code cannot be replayed.
func Verify(secret []byte, code string, t time.Time, lastUsed int64) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != digits {
		return 0, false
	}
	now := Step(t)
	for _, s := range []int64{now - 1, now, now + 1} {
		if s <= lastUsed {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(Code(secret, s)), []byte(code)) == 1 {
			return s, true
		}
	}
	return 0, false
}
