package proxied

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestProxiedConnectionsNameTheCaller(t *testing.T) {
	ln, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 2)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				b := make([]byte, 5)
				if _, err := io.ReadFull(c, b); err != nil {
					got <- "refused"
					return
				}
				got <- c.RemoteAddr().String() + " " + string(b)
			}()
		}
	}()
	send := func(data string) string {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		c.Write([]byte(data))
		select {
		case s := <-got:
			return s
		case <-time.After(3 * time.Second):
			return "timeout"
		}
	}
	// v1 header, then the protocol's own bytes.
	if s := send("PROXY TCP4 203.0.113.5 10.0.0.2 51234 2323\r\nhello"); s != "203.0.113.5:51234 hello" {
		t.Errorf("v1: %q", s)
	}
	// v2 (binary), as frp sends with proxy_protocol_version = "v2".
	v2 := []byte("\r\n\r\n\x00\r\nQUIT\n\x21\x11\x00\x0c")
	v2 = append(v2, 198, 51, 100, 7, 10, 0, 0, 2, 0x1f, 0x90, 0x09, 0x1b)
	if s := send(string(v2) + "hello"); s != "198.51.100.7:8080 hello" {
		t.Errorf("v2: %q", s)
	}
	// No header: refused, nobody claims an address here.
	if s := send("hello"); s != "refused" {
		t.Errorf("without a header: %q", s)
	}
}
