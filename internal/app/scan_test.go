package app_test

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"

	"shopee-automation/internal/platform/config"
)

// fakeClamd answers INSTREAM like clamd: FOUND for the EICAR marker.
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
				_, _ = r.ReadString(0)
				var body strings.Builder
				for {
					var n uint32
					if binary.Read(r, binary.BigEndian, &n) != nil || n == 0 {
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

const eicar = `X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`

func TestUploadsAreScanned(t *testing.T) {
	addr := fakeClamd(t)
	e := newEnv(t, func(c *config.Config) { c.ClamAVAddr = addr })
	alice := e.signUp("alice@a.example", "Alice")
	org := alice.createOrg("Org A", "org-a")
	pid := createProduct(alice, org, baseProduct("S-1"), true) // a clean image passes
	png := testPNG(10, 10)
	infected := append(append([]byte{}, png...), []byte(eicar)...)
	if r := alice.upload("/orgs/"+org+"/products/"+pid+"/images", "x.png", infected, nil); r.Status != 422 || !strings.Contains(string(r.Body), "malware") {
		t.Fatalf("infected image: %d %s", r.Status, r.Body)
	}
	if r := alice.upload("/orgs/"+org+"/imports", "p.csv", []byte("sku,name\n"+eicar+",x\n"), nil); r.Status != 422 {
		t.Fatalf("infected import: %d %s", r.Status, r.Body)
	}

	// A configured scanner that cannot be reached refuses uploads.
	down := newEnv(t, func(c *config.Config) { c.ClamAVAddr = "127.0.0.1:1" })
	bob := down.signUp("bob@b.example", "Bob")
	orgB := bob.createOrg("Org B", "org-b")
	var p productResp
	bob.must(201, "POST", "/orgs/"+orgB+"/products", baseProduct("S-2")).json(t, &p)
	if r := bob.upload("/orgs/"+orgB+"/products/"+p.Data.ID+"/images", "x.png", png, nil); r.Status != 503 {
		t.Fatalf("scanner down: %d %s", r.Status, r.Body)
	}
}
