package envelope

import (
	"bytes"
	"crypto/rand"
	"errors"
	"testing"
)

func key(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return b
}

func TestRoundTrip(t *testing.T) {
	k, err := NewKeyring("k1", map[string][]byte{"k1": key(t)})
	if err != nil {
		t.Fatal(err)
	}
	row, wk, err := k.NewRow("shop_credentials", "row-1")
	if err != nil {
		t.Fatal(err)
	}
	ct, err := row.Seal("access_token", []byte("secret-token"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, []byte("secret-token")) {
		t.Fatal("ciphertext contains plaintext")
	}
	again, err := k.OpenRow("shop_credentials", "row-1", wk)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := again.Open("access_token", ct)
	if err != nil || string(pt) != "secret-token" {
		t.Fatalf("open: %q %v", pt, err)
	}
}

func TestBindingAndTampering(t *testing.T) {
	k, _ := NewKeyring("k1", map[string][]byte{"k1": key(t)})
	row, wk, _ := k.NewRow("shop_credentials", "row-1")
	ct, _ := row.Seal("access_token", []byte("secret"))

	if _, err := row.Open("refresh_token", ct); !errors.Is(err, ErrDecrypt) {
		t.Errorf("other column: %v", err)
	}
	if _, err := k.OpenRow("shop_credentials", "row-2", wk); !errors.Is(err, ErrDecrypt) {
		t.Errorf("other row: %v", err)
	}
	if _, err := k.OpenRow("shopee_apps", "row-1", wk); !errors.Is(err, ErrDecrypt) {
		t.Errorf("other table: %v", err)
	}
	bad := append([]byte(nil), ct...)
	bad[len(bad)-1] ^= 1
	if _, err := row.Open("access_token", bad); !errors.Is(err, ErrDecrypt) {
		t.Errorf("tampered: %v", err)
	}
	if _, err := row.Open("access_token", ct[:5]); !errors.Is(err, ErrDecrypt) {
		t.Errorf("short: %v", err)
	}
	other, _ := NewKeyring("k1", map[string][]byte{"k1": key(t)})
	if _, err := other.OpenRow("shop_credentials", "row-1", wk); !errors.Is(err, ErrDecrypt) {
		t.Errorf("wrong kek: %v", err)
	}
	if _, err := k.OpenRow("shop_credentials", "row-1", WrappedKey{KEKID: "nope", DEK: wk.DEK}); !errors.Is(err, ErrDecrypt) {
		t.Errorf("unknown kek: %v", err)
	}
}

func TestRotation(t *testing.T) {
	k1, k2 := key(t), key(t)
	old, _ := NewKeyring("k1", map[string][]byte{"k1": k1})
	row, wk, _ := old.NewRow("t", "r")
	ct, _ := row.Seal("c", []byte("v"))

	both, _ := NewKeyring("k2", map[string][]byte{"k1": k1, "k2": k2})
	re, err := both.Rewrap("t", "r", wk)
	if err != nil || re.KEKID != "k2" {
		t.Fatalf("rewrap: %+v %v", re, err)
	}
	onlyNew, _ := NewKeyring("k2", map[string][]byte{"k2": k2})
	r2, err := onlyNew.OpenRow("t", "r", re)
	if err != nil {
		t.Fatal(err)
	}
	if pt, err := r2.Open("c", ct); err != nil || string(pt) != "v" {
		t.Fatalf("after rotation: %q %v", pt, err)
	}
}

func TestParseKeyring(t *testing.T) {
	good := GenerateKey()
	cases := []struct {
		spec, active string
		ok           bool
	}{
		{"k1:" + good, "", true},
		{"k1:" + good, "k1", true},
		{"k1:" + good + ",k2:" + GenerateKey(), "k2", true},
		{"k1:" + good + ",k2:" + GenerateKey(), "", false}, // ambiguous active
		{"k1:" + good, "k9", false},
		{"", "", false},
		{"k1:short", "", false},
		{"bad id:" + good, "", false},
		{"k1:" + good + ",k1:" + good, "k1", false},
	}
	for _, c := range cases {
		_, err := ParseKeyring(c.spec, c.active)
		if (err == nil) != c.ok {
			t.Errorf("ParseKeyring(%q, %q) err=%v, want ok=%v", c.spec, c.active, err, c.ok)
		}
	}
}
