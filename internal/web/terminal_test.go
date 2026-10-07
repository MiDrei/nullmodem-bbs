package web

import (
	"bytes"
	"context"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/midrei/nullmodem-bbs/internal/telnet"
)

func TestWebTerminalBridgesToTelnet(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	seen := make(chan string, 1)
	tsrv := &telnet.Server{Addr: addr, Handler: func(s *telnet.Session) {
		s.Write([]byte("Welcome \xb0\xb1\xb2\r\nhandle: "))
		buf := make([]byte, 5)
		n := 0
		for n < 5 {
			m, err := s.Read(buf[n:])
			if err != nil {
				return
			}
			n += m
		}
		w, h := s.WindowSize()
		seen <- s.RemoteAddr().String() + " " + s.Protocol() + " " + string(buf) + " " + strings.Repeat("x", 0) +
			itoa(int64(w)) + "x" + itoa(int64(h))
		time.Sleep(200 * time.Millisecond)
	}}
	go tsrv.ListenAndServe()
	time.Sleep(100 * time.Millisecond)

	srv, _, _ := newTestServer(t)
	srv.TerminalAddr = addr
	hs := httptest.NewServer(srv.Routes())
	defer hs.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(hs.URL, "http")+"/api/terminal", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	ws.Write(ctx, websocket.MessageText, []byte(`{"cols":100,"rows":30}`))

	var got []byte
	for !bytes.Contains(got, []byte("handle: ")) {
		_, data, err := ws.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v (so far %q)", err, got)
		}
		got = append(got, data...)
	}
	if bytes.IndexByte(got, 0xff) >= 0 {
		t.Errorf("telnet negotiation reached the browser: %q", got)
	}
	if !bytes.Contains(got, []byte("Welcome \xb0\xb1\xb2")) {
		t.Errorf("CP437 bytes changed: %q", got)
	}
	ws.Write(ctx, websocket.MessageBinary, []byte("alice"))
	select {
	case v := <-seen:
		if !strings.HasPrefix(v, "127.0.0.1:0 web alice ") {
			t.Fatalf("BBS saw %q", v)
		}
		if !strings.HasSuffix(v, "100x30") {
			t.Errorf("window size not passed on: %q", v)
		}
	case <-ctx.Done():
		t.Fatal("keys never reached the BBS")
	}
}
