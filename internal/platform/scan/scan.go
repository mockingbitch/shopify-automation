// Package scan checks uploaded files for malware with clamd (ClamAV) over
// its INSTREAM protocol. It is optional: without CLAMAV_ADDR nothing is
// scanned. When configured, an unreachable scanner rejects the upload
// (fail closed).
package scan

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// ErrInfected means the scanner found malware.
var ErrInfected = errors.New("scan: malware found")

// Scanner checks a file.
type Scanner interface {
	Scan(ctx context.Context, data []byte) error
}

// Clamd talks to clamd over TCP.
type Clamd struct {
	Addr    string
	Timeout time.Duration
}

const chunk = 64 << 10

// Scan implements Scanner. It returns ErrInfected (wrapped with the
// signature name) or a connection/protocol error.
func (c Clamd) Scan(ctx context.Context, data []byte) error {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", c.Addr)
	if err != nil {
		return fmt.Errorf("scan: connect: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	if _, err := conn.Write([]byte("zINSTREAM\x00")); err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	var size [4]byte
	for off := 0; off < len(data); off += chunk {
		end := min(off+chunk, len(data))
		binary.BigEndian.PutUint32(size[:], uint32(end-off)) // #nosec G115 -- chunk ≤ 64 KiB
		if _, err := conn.Write(size[:]); err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		if _, err := conn.Write(data[off:end]); err != nil {
			return fmt.Errorf("scan: %w", err)
		}
	}
	binary.BigEndian.PutUint32(size[:], 0)
	if _, err := conn.Write(size[:]); err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	reply, err := io.ReadAll(io.LimitReader(conn, 4096))
	if err != nil {
		return fmt.Errorf("scan: read reply: %w", err)
	}
	r := strings.TrimSpace(string(bytes.TrimRight(reply, "\x00")))
	switch {
	case strings.HasSuffix(r, "OK"):
		return nil
	case strings.HasSuffix(r, "FOUND"):
		name := strings.TrimSuffix(strings.TrimPrefix(r, "stream: "), " FOUND")
		return fmt.Errorf("%w: %s", ErrInfected, name)
	default:
		return fmt.Errorf("scan: unexpected reply %q", r)
	}
}
