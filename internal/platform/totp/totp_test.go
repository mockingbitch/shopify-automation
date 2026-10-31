package totp

import (
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B test vectors (SHA-1, last 6 of the 8 digits).
func TestRFC6238Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for _, c := range []struct {
		unix int64
		want string
	}{{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"}, {1234567890, "005924"}, {2000000000, "279037"}, {20000000000, "353130"}} {
		if got := Code(secret, Step(time.Unix(c.unix, 0))); got != c.want {
			t.Errorf("t=%d: %s, want %s", c.unix, got, c.want)
		}
	}
}

func TestVerify(t *testing.T) {
	secret := NewSecret()
	now := time.Unix(1_800_000_000, 0)
	code := Code(secret, Step(now))
	step, ok := Verify(secret, code, now, 0)
	if !ok || step != Step(now) {
		t.Fatal("current code rejected")
	}
	if _, ok := Verify(secret, code, now, step); ok {
		t.Fatal("replay accepted")
	}
	if _, ok := Verify(secret, Code(secret, Step(now)-1), now, 0); !ok {
		t.Fatal("previous step (drift) rejected")
	}
	if _, ok := Verify(secret, Code(secret, Step(now)-2), now, 0); ok {
		t.Fatal("old code accepted")
	}
	if _, ok := Verify(secret, "12345", now, 0); ok {
		t.Fatal("short code accepted")
	}
	if !strings.HasPrefix(URI(secret, "Shopee Automation", "a@b.c"), "otpauth://totp/Shopee%20Automation:a@b.c?") {
		t.Fatal(URI(secret, "Shopee Automation", "a@b.c"))
	}
}
