package bbs

import (
	"bytes"
	"net"
	"testing"
)

// fakeConn is a minimal in-memory Conn for exercising Terminal without
// a real telnet/SSH transport.
type fakeConn struct {
	in  *bytes.Reader
	out bytes.Buffer
}

func newFakeConn(input string) *fakeConn {
	return &fakeConn{in: bytes.NewReader([]byte(input))}
}

func (c *fakeConn) Read(p []byte) (int, error)  { return c.in.Read(p) }
func (c *fakeConn) Write(p []byte) (int, error) { return c.out.Write(p) }
func (c *fakeConn) Close() error                { return nil }
func (c *fakeConn) RemoteAddr() net.Addr        { return &net.TCPAddr{} }
func (c *fakeConn) TermType() string            { return "test" }
func (c *fakeConn) WindowSize() (int, int)      { return 80, 24 }

// TestReadLineCRLFDoesNotLeakIntoNextLine is a regression test: a
// trailing LF after CR must be consumed as part of the same line
// terminator, not returned as a spurious empty line on the next
// ReadLine call.
func TestReadLineCRLFDoesNotLeakIntoNextLine(t *testing.T) {
	conn := newFakeConn("first\r\nsecond\r\n")
	term := NewTerminal(conn)

	got, err := term.ReadLine(false)
	if err != nil {
		t.Fatalf("first ReadLine: %v", err)
	}
	if got != "first" {
		t.Fatalf("first ReadLine = %q, want %q", got, "first")
	}

	got, err = term.ReadLine(false)
	if err != nil {
		t.Fatalf("second ReadLine: %v", err)
	}
	if got != "second" {
		t.Fatalf("second ReadLine = %q, want %q (leaked CRLF byte?)", got, "second")
	}
}

func TestReadLineBareLF(t *testing.T) {
	conn := newFakeConn("unix-style\n")
	term := NewTerminal(conn)

	got, err := term.ReadLine(false)
	if err != nil {
		t.Fatalf("ReadLine: %v", err)
	}
	if got != "unix-style" {
		t.Fatalf("ReadLine = %q, want %q", got, "unix-style")
	}
}

func TestReadLineBackspaceEditsBuffer(t *testing.T) {
	conn := newFakeConn("abc\x08\x08d\r\n")
	term := NewTerminal(conn)

	got, err := term.ReadLine(false)
	if err != nil {
		t.Fatalf("ReadLine: %v", err)
	}
	if got != "ad" {
		t.Fatalf("ReadLine = %q, want %q", got, "ad")
	}
}
