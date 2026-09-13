package bbs

import (
	"bytes"
	"net"
	"testing"
)

// fakeConn is a minimal in-memory Conn for exercising Terminal without
// a real telnet/SSH transport.
type fakeConn struct {
	in     *bytes.Reader
	out    bytes.Buffer
	width  int
	height int
}

func newFakeConn(input string) *fakeConn {
	return &fakeConn{in: bytes.NewReader([]byte(input)), width: 80, height: 24}
}

func (c *fakeConn) Read(p []byte) (int, error)  { return c.in.Read(p) }
func (c *fakeConn) Write(p []byte) (int, error) { return c.out.Write(p) }
func (c *fakeConn) Close() error                { return nil }
func (c *fakeConn) RemoteAddr() net.Addr        { return &net.TCPAddr{} }
func (c *fakeConn) TermType() string            { return "test" }
func (c *fakeConn) WindowSize() (int, int)      { return c.width, c.height }

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

func TestWidthCapsAtMaxWidthMinusMargin(t *testing.T) {
	// Real-world finding: SyncTERM reported a NAWS width larger than
	// what it actually rendered without wrapping after its window was
	// resized. Trusting that value wrapped a full-width screen's
	// trailing border character onto the next line. Width must cap at
	// maxWidth rather than pass an oversized value through.
	conn := newFakeConn("")
	conn.width = 204
	term := NewTerminal(conn)

	want := maxWidth - wrapMargin
	if got := term.Width(); got != want {
		t.Fatalf("Width() = %d, want capped at %d", got, want)
	}
}

func TestWidthRespectsNarrowerClient(t *testing.T) {
	conn := newFakeConn("")
	conn.width = 40
	term := NewTerminal(conn)

	want := 40 - wrapMargin
	if got := term.Width(); got != want {
		t.Fatalf("Width() = %d, want %d (genuinely narrow clients must still be respected)", got, want)
	}
}

func TestWidthFallsBackWhenUnreported(t *testing.T) {
	conn := newFakeConn("")
	conn.width = 0
	term := NewTerminal(conn)

	want := maxWidth - wrapMargin
	if got := term.Width(); got != want {
		t.Fatalf("Width() = %d, want fallback %d", got, want)
	}
}

func TestWidthLeavesLastColumnUnusedEvenAtExactMax(t *testing.T) {
	// Also found in real testing: even a client correctly reporting
	// exactly maxWidth (80) still wrapped a full-width line. Width
	// must never return the full negotiated/capped value unmargined.
	conn := newFakeConn("")
	conn.width = maxWidth
	term := NewTerminal(conn)

	if got := term.Width(); got != maxWidth-wrapMargin {
		t.Fatalf("Width() = %d, want %d (margin must apply even at exactly maxWidth)", got, maxWidth-wrapMargin)
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

func TestReadKeyRecognizesArrowKeys(t *testing.T) {
	conn := newFakeConn("\x1b[A\x1b[B\x1b[C\x1b[D")
	term := NewTerminal(conn)

	want := []KeyType{KeyUp, KeyDown, KeyRight, KeyLeft}
	for i, w := range want {
		key, err := term.ReadKey()
		if err != nil {
			t.Fatalf("ReadKey %d: %v", i, err)
		}
		if key.Type != w {
			t.Fatalf("ReadKey %d = %v, want %v", i, key.Type, w)
		}
	}
}

func TestReadKeyRecognizesEnterCharAndBackspace(t *testing.T) {
	conn := newFakeConn("q\r\n\x08")
	term := NewTerminal(conn)

	key, err := term.ReadKey()
	if err != nil || key.Type != KeyChar || key.Rune != 'q' {
		t.Fatalf("ReadKey 1 = %+v, err=%v; want KeyChar 'q'", key, err)
	}
	key, err = term.ReadKey()
	if err != nil || key.Type != KeyEnter {
		t.Fatalf("ReadKey 2 = %+v, err=%v; want KeyEnter (CRLF collapsed)", key, err)
	}
	key, err = term.ReadKey()
	if err != nil || key.Type != KeyBackspace {
		t.Fatalf("ReadKey 3 = %+v, err=%v; want KeyBackspace", key, err)
	}
}

func TestReadKeyDoesNotLeakEscapeLookaheadByte(t *testing.T) {
	// A bare Escape (not followed by '[') must push the byte after it
	// back for the next ReadKey call, the same lookahead discipline
	// ReadLine uses for CRLF.
	conn := newFakeConn("\x1bxq")
	term := NewTerminal(conn)

	key, err := term.ReadKey()
	if err != nil || key.Type != KeyEscape {
		t.Fatalf("ReadKey 1 = %+v, err=%v; want KeyEscape", key, err)
	}
	key, err = term.ReadKey()
	if err != nil || key.Type != KeyChar || key.Rune != 'x' {
		t.Fatalf("ReadKey 2 = %+v, err=%v; want KeyChar 'x' (pushed-back byte)", key, err)
	}
	key, err = term.ReadKey()
	if err != nil || key.Type != KeyChar || key.Rune != 'q' {
		t.Fatalf("ReadKey 3 = %+v, err=%v; want KeyChar 'q'", key, err)
	}
}
