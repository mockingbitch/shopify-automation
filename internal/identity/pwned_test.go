package identity

import (
	"context"
	"crypto/sha1" // #nosec G505 -- matches the range API
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPwnedRange(t *testing.T) {
	sum := sha1.Sum([]byte("password123456")) // #nosec G401 -- test of the range API
	h := strings.ToUpper(hex.EncodeToString(sum[:]))
	var gotPath, padding string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, padding = r.URL.Path, r.Header.Get("Add-Padding")
		_, _ = w.Write([]byte("0000000000000000000000000000000000A:0\r\n" + h[5:] + ":42\r\n"))
	}))
	defer srv.Close()
	p := PwnedRange{BaseURL: srv.URL + "/range/"}
	bad, err := p.Breached(context.Background(), "password123456")
	if err != nil || !bad {
		t.Fatalf("breached: %v %v", bad, err)
	}
	if gotPath != "/range/"+h[:5] || padding != "true" {
		t.Fatalf("request: %s padding=%s (only the 5-char prefix may be sent)", gotPath, padding)
	}
	if bad, _ := p.Breached(context.Background(), "a much better passphrase"); bad {
		t.Fatal("clean password reported")
	}
	srv.Close()
	if _, err := p.Breached(context.Background(), "x"); err == nil {
		t.Fatal("unreachable service should return an error (the caller fails open)")
	}
}
