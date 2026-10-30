package identity

import (
	"strings"
	"testing"
)

var fastArgon = Argon2Params{MemoryKiB: 64, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

func TestHashAndVerify(t *testing.T) {
	h := Hasher{Params: fastArgon}
	enc, err := h.Hash("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Fatalf("unexpected format %q", enc)
	}
	enc2, _ := h.Hash("correct horse battery")
	if enc == enc2 {
		t.Fatal("salt must differ between hashes")
	}
	if ok, rehash, err := h.Verify("correct horse battery", enc); !ok || rehash || err != nil {
		t.Fatalf("verify good: ok=%v rehash=%v err=%v", ok, rehash, err)
	}
	if ok, _, err := h.Verify("wrong password!!", enc); ok || err != nil {
		t.Fatalf("verify bad: ok=%v err=%v", ok, err)
	}
}

func TestVerifyFlagsWeakParams(t *testing.T) {
	weak := Hasher{Params: fastArgon}
	enc, _ := weak.Hash("correct horse battery")
	strong := Hasher{Params: Argon2Params{MemoryKiB: 128, Time: 2, Threads: 1, SaltLen: 16, KeyLen: 32}}
	ok, rehash, err := strong.Verify("correct horse battery", enc)
	if !ok || !rehash || err != nil {
		t.Fatalf("ok=%v rehash=%v err=%v", ok, rehash, err)
	}
}

func TestVerifyMalformed(t *testing.T) {
	h := Hasher{Params: fastArgon}
	for _, bad := range []string{"", "plain", "$argon2i$v=19$m=1,t=1,p=1$a$b", "$argon2id$v=18$m=1,t=1,p=1$YQ$YQ", "$argon2id$v=19$m=x$YQ$YQ"} {
		if ok, _, err := h.Verify("x", bad); ok || err == nil {
			t.Errorf("%q: ok=%v err=%v", bad, ok, err)
		}
	}
}

func TestNewToken(t *testing.T) {
	a, ha := NewToken()
	b, _ := NewToken()
	if a == b || len(a) != 43 || len(ha) != 32 {
		t.Fatalf("token %q hash len %d", a, len(ha))
	}
	if string(HashToken(a)) != string(ha) {
		t.Fatal("hash mismatch")
	}
}
