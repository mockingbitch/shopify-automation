package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func pngBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// pngHeader is a PNG signature plus an IHDR chunk declaring w x h pixels,
// with no image data: enough for DecodeConfig.
func pngHeader(w, h uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 6 // 8-bit RGBA
	var b bytes.Buffer
	b.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
	_ = binary.Write(&b, binary.BigEndian, uint32(13))
	chunk := append([]byte("IHDR"), ihdr...)
	b.Write(chunk)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

func TestSanitize(t *testing.T) {
	// JPEG with appended payload (polyglot): re-encoding drops it.
	var j bytes.Buffer
	_ = jpeg.Encode(&j, image.NewGray(image.Rect(0, 0, 10, 10)), nil)
	poly := append(j.Bytes(), []byte("<script>alert(1)</script>")...)
	out, err := Sanitize(poly)
	if err != nil || out.ContentType != "image/jpeg" || bytes.Contains(out.Data, []byte("script")) || out.Width != 10 {
		t.Fatalf("jpeg: %+v %v", out.ContentType, err)
	}
	p, err := Sanitize(pngBytes(4, 3))
	if err != nil || p.ContentType != "image/png" || p.Height != 3 {
		t.Fatalf("png: %v", err)
	}
	if _, err := Sanitize([]byte("GIF89a....")); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("gif: %v", err)
	}
	if _, err := Sanitize(append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 40)...)); !errors.Is(err, ErrBroken) {
		t.Fatalf("broken png: %v", err)
	}
	// A PNG header declaring 8000x8000 pixels is refused before decoding.
	big := pngHeader(8000, 8000)
	if _, err := Sanitize(big); !errors.Is(err, ErrTooManyPixel) {
		t.Fatalf("too many pixels: %v", err)
	}
	if _, err := Sanitize(make([]byte, MaxBytes+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("too large: %v", err)
	}
}

func TestPublic(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8": true, "2606:4700::1111": true,
		"127.0.0.1": false, "10.1.2.3": false, "172.16.0.1": false, "192.168.1.1": false, "169.254.169.254": false,
		"100.64.0.1": false, "0.0.0.0": false, "::1": false, "fe80::1": false, "fd00::1": false, "::ffff:127.0.0.1": false,
		"64:ff9b::a00:1": false,
	} {
		if got := Public(netip.MustParseAddr(addr)); got != want {
			t.Errorf("Public(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestFetcherBlocksInternalTargets(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(pngBytes(2, 2)) }))
	defer srv.Close()
	f := NewFetcher(5*time.Second, false)
	ctx := context.Background()
	if _, err := f.Fetch(ctx, srv.URL+"/x.png"); !errors.Is(err, ErrBlockedHost) {
		t.Fatalf("loopback: %v", err)
	}
	if _, err := f.Fetch(ctx, "http://example.com/x.png"); !errors.Is(err, ErrNotHTTPS) {
		t.Fatalf("http: %v", err)
	}
	if _, err := f.Fetch(ctx, "https://169.254.169.254/latest/meta-data"); !errors.Is(err, ErrBlockedHost) {
		t.Fatalf("metadata endpoint: %v", err)
	}
}

func TestTestFetcher(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/big" {
			_, _ = w.Write(make([]byte, MaxBytes+10))
			return
		}
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(pngBytes(2, 2))
	}))
	defer srv.Close()
	f := NewTestFetcher()
	ctx := context.Background()
	if b, err := f.Fetch(ctx, srv.URL+"/ok.png"); err != nil || Sniff(b) != "png" {
		t.Fatalf("ok: %v", err)
	}
	if _, err := f.Fetch(ctx, srv.URL+"/big"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("big: %v", err)
	}
	if _, err := f.Fetch(ctx, srv.URL+"/missing"); err == nil {
		t.Fatal("404 accepted")
	}
}
