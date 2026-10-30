// Package media downloads and sanitizes product images.
//
// Downloads go through a fetcher that only speaks HTTPS and refuses to
// connect to private, loopback, link-local and other internal addresses.
// The check runs at connect time on the resolved IP, so DNS rebinding and
// redirects to internal hosts are blocked too (risk R8).
//
// Images are identified by magic bytes, size-checked, dimension-checked
// before decoding (decompression bombs), then decoded and re-encoded. The
// output is a fresh JPEG or PNG without metadata or trailing payloads (R7).
package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"

	_ "golang.org/x/image/webp" // register WebP decoding
)

// Limits from FR-PRD-6 / api-design §4 and the media_space.upload_image
// reference (≤ 10 MB, JPG/JPEG/PNG).
const (
	MaxBytes  = 10 << 20
	MaxPixels = 40_000_000
)

// Errors with messages safe to show in row errors.
var (
	ErrNotHTTPS     = errors.New("image URL must use https")
	ErrBlockedHost  = errors.New("image URL points to an address that is not allowed")
	ErrTooLarge     = errors.New("image is larger than 10 MB")
	ErrUnsupported  = errors.New("image must be JPEG, PNG or WebP")
	ErrTooManyPixel = errors.New("image has more than 40 megapixels")
	ErrBroken       = errors.New("image could not be decoded")
)

// Fetcher downloads images from the internet safely.
type Fetcher struct {
	Client    *http.Client
	allowHTTP bool // tests only
}

// NewFetcher builds a fetcher. allowPrivate is for tests only.
func NewFetcher(timeout time.Duration, allowPrivate bool) *Fetcher {
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		if allowPrivate {
			return nil
		}
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return ErrBlockedHost
		}
		ip, err := netip.ParseAddr(host)
		if err != nil || !Public(ip) {
			return ErrBlockedHost
		}
		return nil
	}}
	tr := &http.Transport{
		DialContext:           dialer.DialContext,
		Proxy:                 nil, // never route through an environment proxy
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConns:          20,
		IdleConnTimeout:       30 * time.Second,
	}
	return &Fetcher{Client: &http.Client{Transport: tr, Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" && !allowPrivate {
				return ErrNotHTTPS
			}
			return nil
		}}}
}

// Public reports whether ip is a publicly routable unicast address.
func Public(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	for _, p := range blocked {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

var blocked = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),   // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64 (could reach IPv4 internals)
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
}

// Fetch downloads raw bytes from an https URL, at most MaxBytes.
func (f *Fetcher) Fetch(ctx context.Context, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, errors.New("image URL is not valid")
	}
	if u.Scheme != "https" && !f.allowHTTP {
		return nil, ErrNotHTTPS
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("image URL is not valid")
	}
	req.Header.Set("Accept", "image/jpeg, image/png, image/webp")
	res, err := f.Client.Do(req)
	if err != nil {
		if errors.Is(err, ErrBlockedHost) {
			return nil, ErrBlockedHost
		}
		if errors.Is(err, ErrNotHTTPS) {
			return nil, ErrNotHTTPS
		}
		return nil, errors.New("image could not be downloaded")
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image download failed with HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, MaxBytes+1))
	if err != nil {
		return nil, errors.New("image could not be downloaded")
	}
	if len(b) > MaxBytes {
		return nil, ErrTooLarge
	}
	return b, nil
}

// NewTestFetcher allows http and private addresses (httptest servers).
func NewTestFetcher() *Fetcher {
	f := NewFetcher(5*time.Second, true)
	f.allowHTTP = true
	return f
}

// Image is a sanitized image.
type Image struct {
	Data        []byte
	ContentType string // image/jpeg or image/png
	Width       int
	Height      int
	SHA256      [32]byte
}

// Sniff returns the format of b from its magic bytes.
func Sniff(b []byte) string {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "jpeg"
	case len(b) >= 8 && bytes.Equal(b[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		return "png"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "webp"
	}
	return ""
}

// Sanitize validates and re-encodes an image.
func Sanitize(b []byte) (Image, error) {
	if len(b) > MaxBytes {
		return Image{}, ErrTooLarge
	}
	format := Sniff(b)
	if format == "" {
		return Image{}, ErrUnsupported
	}
	cfg, got, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || got != format {
		return Image{}, ErrBroken
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return Image{}, ErrBroken
	}
	if int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return Image{}, ErrTooManyPixel
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return Image{}, ErrBroken
	}
	var out bytes.Buffer
	ct := "image/jpeg"
	if format == "png" {
		ct = "image/png"
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, img)
	} else {
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: 92})
	}
	if err != nil {
		return Image{}, ErrBroken
	}
	if out.Len() > MaxBytes {
		return Image{}, ErrTooLarge
	}
	bounds := img.Bounds()
	return Image{Data: out.Bytes(), ContentType: ct, Width: bounds.Dx(), Height: bounds.Dy(), SHA256: sha256.Sum256(out.Bytes())}, nil
}
