package telnet

import (
	"io"
	"net"
	"testing"
)

// TestWriteDoublesLiteralIAC is a regression test: a literal 0xFF
// (IAC) byte in application data -- e.g. internal/zmodem's binary
// file transfers -- must be doubled per RFC 854/856 so the client's
// own telnet layer doesn't misread it as the start of a command
// sequence. Confirmed live: without this, a Zmodem transfer
// containing a 0xFF byte corrupted the stream the client's own Zmodem
// receiver saw.
func TestWriteDoublesLiteralIAC(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	sess := newSession(server)

	payload := []byte{0x01, 0xFF, 0x02, 0xFF, 0xFF, 0x03}
	done := make(chan struct{})
	go func() {
		defer close(done)
		n, err := sess.Write(payload)
		if err != nil {
			t.Errorf("Write: %v", err)
			return
		}
		if n != len(payload) {
			t.Errorf("Write returned n=%d, want %d (the logical byte count, not the doubled wire count)", n, len(payload))
		}
	}()

	want := []byte{0x01, 0xFF, 0xFF, 0x02, 0xFF, 0xFF, 0xFF, 0xFF, 0x03}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatalf("reading from client side: %v", err)
	}
	<-done

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("wire bytes = % x, want % x (every 0xFF doubled)", got, want)
		}
	}
}

// TestWriteLeavesOrdinaryDataUnmodified confirms the common case (no
// 0xFF byte at all) isn't altered or slowed down by the escaping
// logic.
func TestWriteLeavesOrdinaryDataUnmodified(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	sess := newSession(server)

	payload := []byte("Hello, BBS!\r\n")
	go func() {
		if _, err := sess.Write(payload); err != nil {
			t.Errorf("Write: %v", err)
		}
	}()

	got := make([]byte, len(payload))
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatalf("reading from client side: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("wire bytes = %q, want %q unchanged", got, payload)
	}
}
