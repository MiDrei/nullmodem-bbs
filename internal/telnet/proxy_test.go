package telnet

import (
	"net"
	"testing"
	"time"
)

// serveOnce runs a telnet server on loopback and returns what the
// handler saw for the first connection, after send was written by
// the client.
func serveOnce(t *testing.T, send string) (addr, protocol, firstByte string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().String()
	ln.Close()
	seen := make(chan [3]string, 1)
	srv := &Server{Addr: port, Handler: func(s *Session) {
		b := make([]byte, 1)
		s.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _ := s.Read(b)
		seen <- [3]string{s.RemoteAddr().String(), s.Protocol(), string(b[:n])}
	}}
	go srv.ListenAndServe()
	var c net.Conn
	for i := 0; i < 50; i++ {
		if c, err = net.Dial("tcp", port); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte(send))
	select {
	case v := <-seen:
		return v[0], v[1], v[2]
	case <-time.After(5 * time.Second):
		t.Fatal("handler never ran")
	}
	return
}

func TestProxyLineFromLoopbackNamesTheCaller(t *testing.T) {
	addr, proto, first := serveOnce(t, "PROXY TCP4 203.0.113.9 127.0.0.1 0 0\r\nx")
	if addr != "203.0.113.9:0" || proto != "web" || first != "x" {
		t.Fatalf("got %q %q %q", addr, proto, first)
	}
}

func TestNoProxyLineKeepsWhatTheClientSent(t *testing.T) {
	addr, proto, first := serveOnce(t, "y")
	if proto != "telnet" || first != "y" || addr[:10] != "127.0.0.1:" {
		t.Fatalf("got %q %q %q", addr, proto, first)
	}
}
