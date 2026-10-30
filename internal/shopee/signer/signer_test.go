package signer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// Official example from the "API calls" developer guide.
func TestBaseStringOfficialExample(t *testing.T) {
	got := BaseString(2001887, "/api/v2/shop/get_shop_info", 1655714431, Shop, "59777174636562737266615546704c6d", 14701711)
	want := "2001887/api/v2/shop/get_shop_info165571443159777174636562737266615546704c6d14701711"
	if got != want {
		t.Fatalf("base string\n got %s\nwant %s", got, want)
	}
	if got := BaseString(2001887, "/api/v2/auth/token/get", 1655714431, Public, "ignored", 1); got != "2001887/api/v2/auth/token/get1655714431" {
		t.Fatalf("public base string: %s", got)
	}
}

func TestSignIsHMACSHA256Hex(t *testing.T) {
	key := []byte("partner-key")
	base := BaseString(1, "/p", 2, Merchant, "tok", 3)
	m := hmac.New(sha256.New, key)
	m.Write([]byte(base))
	if want := hex.EncodeToString(m.Sum(nil)); Sign(key, 1, "/p", 2, Merchant, "tok", 3) != want {
		t.Fatal("sign mismatch")
	}
}

func TestEveryFieldChangesSign(t *testing.T) {
	key := []byte("k")
	base := Sign(key, 1, "/api/v2/x", 100, Shop, "tok", 9)
	variants := map[string]string{
		"key":       Sign([]byte("k2"), 1, "/api/v2/x", 100, Shop, "tok", 9),
		"partner":   Sign(key, 2, "/api/v2/x", 100, Shop, "tok", 9),
		"path":      Sign(key, 1, "/api/v2/y", 100, Shop, "tok", 9),
		"timestamp": Sign(key, 1, "/api/v2/x", 101, Shop, "tok", 9),
		"token":     Sign(key, 1, "/api/v2/x", 100, Shop, "tok2", 9),
		"shop":      Sign(key, 1, "/api/v2/x", 100, Shop, "tok", 8),
		"kind":      Sign(key, 1, "/api/v2/x", 100, Public, "tok", 9),
	}
	for name, v := range variants {
		if v == base {
			t.Errorf("changing %s does not change the sign", name)
		}
	}
	if !Equal(base, Sign(key, 1, "/api/v2/x", 100, Shop, "tok", 9)) || Equal(base, variants["path"]) {
		t.Fatal("Equal")
	}
}
