package telnet

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
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

// TestNegotiateInitialDoesNotRequestBinary confirms negotiateInitial
// never asks for TRANSMIT-BINARY -- see this package's file-level note
// on why it isn't used at all.
func TestNegotiateInitialDoesNotRequestBinary(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	sess := newSession(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := sess.negotiateInitial(); err != nil {
			t.Errorf("negotiateInitial: %v", err)
		}
	}()

	got := make([]byte, 12)
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatalf("reading negotiation bytes: %v", err)
	}
	<-done

	if bytes.Contains(got, []byte{iac, will, optBinary}) || bytes.Contains(got, []byte{iac, do, optBinary}) {
		t.Fatalf("negotiateInitial bytes = % x, want no BINARY request for the whole session", got)
	}
}

// TestClientVolunteeredBinaryOfferDoesNotDisruptOrdinaryInput is a
// regression test for a real bug found live: SyncTERM volunteers
// WILL/DO BINARY completely unprompted, in the same negotiation burst
// as other options, before any Zmodem transfer is even in question --
// and an earlier version of Read, upon processing one of those,
// flipped an internal flag that made it stop checking the REST of
// that same burst for further IAC commands at all, misreading them as
// literal application data instead. Confirmed live: this rendered as
// garbled characters and delayed/missed Enter keys at the very first
// "Enter your handle" prompt, before any Zmodem transfer had started.
// This package no longer treats TRANSMIT-BINARY specially at all (see
// its file-level note), so this now also doubles as confirmation that
// an unprompted offer is simply, harmlessly declined.
func TestClientVolunteeredBinaryOfferDoesNotDisruptOrdinaryInput(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	sess := newSession(server)

	// One burst, unprompted by us: WILL BINARY, DO BINARY, then one
	// ordinary application byte -- exactly what a real client can (and
	// SyncTERM does) send before any transfer.
	burst := []byte{iac, will, optBinary, iac, do, optBinary, 'A'}
	go func() {
		if _, err := client.Write(burst); err != nil {
			t.Errorf("client Write: %v", err)
			return
		}
		// Drain Session's own two decline replies (IAC DONT BINARY, IAC
		// WONT BINARY) -- negotiate's send() blocks on this synchronous
		// pipe until something reads them, same as a real client would.
		ack := make([]byte, 6)
		if _, err := io.ReadFull(client, ack); err != nil {
			t.Errorf("draining decline replies: %v", err)
			return
		}
		want := []byte{iac, dont, optBinary, iac, wont, optBinary}
		if !bytes.Equal(ack, want) {
			t.Errorf("decline replies = % x, want % x", ack, want)
		}
	}()

	type result struct {
		n   int
		err error
	}
	got := make([]byte, 10)
	readDone := make(chan result, 1)
	go func() {
		n, err := sess.Read(got)
		readDone <- result{n, err}
	}()

	select {
	case res := <-readDone:
		if res.err != nil {
			t.Fatalf("Read: %v", res.err)
		}
		if res.n != 1 || got[0] != 'A' {
			t.Fatalf("Read = % x (n=%d), want just \"A\" (n=1) -- the client's own negotiation reply must not leak through as data", got[:res.n], res.n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read never returned the application byte")
	}
}

// TestReadUnescapesDoubledIAC is a regression test: an earlier version
// of Read's command dispatch, upon seeing a doubled IAC (IAC IAC --
// the wire form of one literal 0xFF data byte per Write's escaping),
// swallowed it silently instead of ever delivering the 0xFF to the
// caller.
func TestReadUnescapesDoubledIAC(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	sess := newSession(server)

	payload := []byte{0x01, iac, iac, 0x02}
	go func() {
		if _, err := client.Write(payload); err != nil {
			t.Errorf("client Write: %v", err)
		}
	}()

	want := []byte{0x01, iac, 0x02}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(sess, got); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Read = % x, want %x (doubled IAC collapsed to one literal 0xFF)", got, want)
	}
}

// TestReadReturnsAvailableDataWithoutWaitingToFillTheBuffer is a
// regression test: an earlier version of Read looped until its whole
// argument slice was full (routinely 4096 bytes, bufio.Reader's
// default fill size) before returning, rather than as soon as some
// data was available and nothing more was immediately buffered. That
// went unnoticed against a fast local rz (this repo's own zmodem
// tests), but against a real client over a real network -- which
// sends one short protocol reply and then legitimately waits for the
// server's next frame -- it blocked for the full duration of that
// wait, confirmed live: a real ZRINIT reply sat unread until an
// unrelated timeout elsewhere forced a spurious retry. Here the
// client writes a short payload and nothing more; Read must return it
// promptly rather than blocking for a fill that will never come.
func TestReadReturnsAvailableDataWithoutWaitingToFillTheBuffer(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	sess := newSession(server)

	payload := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	go func() {
		if _, err := client.Write(payload); err != nil {
			t.Errorf("client Write: %v", err)
		}
	}()

	got := make([]byte, 4096)
	type result struct {
		n   int
		err error
	}
	readDone := make(chan result, 1)
	go func() {
		n, err := sess.Read(got)
		readDone <- result{n, err}
	}()

	select {
	case res := <-readDone:
		if res.err != nil {
			t.Fatalf("Read: %v", res.err)
		}
		if res.n != len(payload) || !bytes.Equal(got[:res.n], payload) {
			t.Fatalf("Read = % x (n=%d), want % x (n=%d)", got[:res.n], res.n, payload, len(payload))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read blocked waiting to fill its buffer instead of returning the data already available")
	}
}
