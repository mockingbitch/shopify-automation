package scan

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
)

// fakeClamd implements enough of INSTREAM: it reads the chunks and answers
// FOUND when the stream contains the EICAR marker.
func fakeClamd(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				r := bufio.NewReader(c)
				cmd, _ := r.ReadString(0)
				if cmd != "zINSTREAM\x00" {
					_, _ = c.Write([]byte("UNKNOWN COMMAND\x00"))
					return
				}
				var body strings.Builder
				for {
					var n uint32
					if err := binary.Read(r, binary.BigEndian, &n); err != nil || n == 0 {
						break
					}
					b := make([]byte, n)
					_, _ = io.ReadFull(r, b)
					body.Write(b)
				}
				if strings.Contains(body.String(), "EICAR-STANDARD-ANTIVIRUS-TEST-FILE") {
					_, _ = c.Write([]byte("stream: Eicar-Test-Signature FOUND\x00"))
					return
				}
				_, _ = c.Write([]byte("stream: OK\x00"))
			}(c)
		}
	}()
	return ln.Addr().String()
}

func TestClamd(t *testing.T) {
	c := Clamd{Addr: fakeClamd(t)}
	big := []byte(strings.Repeat("a", 200<<10)) // several chunks
	if err := c.Scan(context.Background(), big); err != nil {
		t.Fatalf("clean: %v", err)
	}
	eicar := []byte(`X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`)
	if err := c.Scan(context.Background(), eicar); !errors.Is(err, ErrInfected) || !strings.Contains(err.Error(), "Eicar") {
		t.Fatalf("infected: %v", err)
	}
	if err := (Clamd{Addr: "127.0.0.1:1"}).Scan(context.Background(), big); err == nil || errors.Is(err, ErrInfected) {
		t.Fatalf("unreachable: %v", err)
	}
}
