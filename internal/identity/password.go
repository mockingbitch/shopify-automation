package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2Params configures Argon2id. Defaults follow the OWASP Password Storage
// Cheat Sheet (m=19 MiB, t=2, p=1).
type Argon2Params struct {
	MemoryKiB uint32
	Time      uint32
	Threads   uint8
	SaltLen   uint32
	KeyLen    uint32
}

// DefaultArgon2 is used in production.
var DefaultArgon2 = Argon2Params{MemoryKiB: 19 * 1024, Time: 2, Threads: 1, SaltLen: 16, KeyLen: 32}

// Hasher hashes and verifies passwords in PHC string format.
type Hasher struct{ Params Argon2Params }

var errBadHash = errors.New("identity: malformed password hash")

// Hash returns "$argon2id$v=19$m=...,t=...,p=...$salt$key".
func (h Hasher) Hash(password string) (string, error) {
	p := h.Params
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("identity: salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, p.Time, p.MemoryKiB, p.Threads, p.KeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, p.MemoryKiB, p.Time, p.Threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify reports whether password matches encoded, and whether the hash
// should be upgraded because it uses weaker parameters than h.Params.
func (h Hasher) Verify(password, encoded string) (ok, needsRehash bool, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, false, errBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, false, errBadHash
	}
	var p Argon2Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Time, &p.Threads); err != nil {
		return false, false, errBadHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, false, errBadHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, false, errBadHash
	}
	got := argon2.IDKey([]byte(password), salt, p.Time, p.MemoryKiB, p.Threads, uint32(len(want))) // #nosec G115 -- len bounded by stored hash
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false, nil
	}
	weaker := p.MemoryKiB < h.Params.MemoryKiB || p.Time < h.Params.Time || p.Threads < h.Params.Threads
	return true, weaker, nil
}
