package identity

import "shopee-automation/internal/platform/securetoken"

// NewToken returns a new opaque token and its hash.
func NewToken() (string, []byte) { return securetoken.New() }

// HashToken returns the stored hash of a token.
func HashToken(token string) []byte { return securetoken.Hash(token) }

func randomBytes(n int) []byte { return securetoken.Bytes(n) }
