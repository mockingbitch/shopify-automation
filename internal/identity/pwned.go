package identity

import (
	"bufio"
	"context"
	"crypto/sha1" // #nosec G505 -- the Pwned Passwords range API is keyed by SHA-1; it is not used for security here
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// PwnedChecker reports whether a password appears in known breaches.
type PwnedChecker interface {
	Breached(ctx context.Context, password string) (bool, error)
}

// PwnedRange queries a k-anonymity range API (Pwned Passwords): only the
// first 5 hex characters of the SHA-1 leave the server.
type PwnedRange struct {
	BaseURL string // e.g. https://api.pwnedpasswords.com/range/
	Client  *http.Client
}

// Breached implements PwnedChecker.
func (p PwnedRange) Breached(ctx context.Context, password string) (bool, error) {
	sum := sha1.Sum([]byte(password)) // #nosec G401 -- required by the range API
	h := strings.ToUpper(hex.EncodeToString(sum[:]))
	prefix, suffix := h[:5], h[5:]
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(p.BaseURL, "/")+"/"+prefix, nil)
	if err != nil {
		return false, err
	}
	// Padding hides the real number of matches from observers.
	req.Header.Set("Add-Padding", "true")
	c := p.Client
	if c == nil {
		c = http.DefaultClient
	}
	res, err := c.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return false, fmt.Errorf("pwned passwords: status %d", res.StatusCode)
	}
	sc := bufio.NewScanner(io.LimitReader(res.Body, 2<<20))
	for sc.Scan() {
		hash, count, ok := strings.Cut(strings.TrimSpace(sc.Text()), ":")
		if ok && strings.EqualFold(hash, suffix) && count != "0" {
			return true, nil
		}
	}
	return false, sc.Err()
}
