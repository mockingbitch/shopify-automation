// Package securetoken generates high-entropy opaque tokens (sessions, email
// links, invitations) and the hashes stored in their place.
package securetoken

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// New returns a random 256-bit URL-safe token and its SHA-256 hash. Only the
// hash is stored; the token is given to the user once.
func New() (token string, hash []byte) {
	token = base64.RawURLEncoding.EncodeToString(Bytes(32))
	return token, Hash(token)
}

// Hash returns the SHA-256 of a token. Tokens are high-entropy, so a fast hash
// is appropriate (a password KDF is not needed).
func Hash(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// Bytes returns n random bytes. It panics if the system random source fails,
// which is unrecoverable.
func Bytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("securetoken: random source failed: " + err.Error())
	}
	return b
}
