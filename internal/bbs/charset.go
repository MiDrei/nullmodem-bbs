package bbs

import (
	"bytes"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/midrei/nullmodem-kit/ansi"
)

// The board works in CP437 throughout: screens, texts, what's stored.
// A caller whose terminal speaks UTF-8 instead (most terminals that
// aren't BBS clients: macOS Terminal, Windows Terminal, PuTTY, an ssh
// in any of them) gets it turned into UTF-8 on the wire, and what they
// type turned back -- charsetConn, below everything else, so screens,
// doors and RLogin all go through it; only file transfers bypass it
// (Terminal.Binary).

// Charset values of user.User.Charset: the caller's choice, "" being
// whatever the terminal answered when asked (detectUTF8).
const (
	CharsetAuto  = ""
	CharsetCP437 = "cp437"
	CharsetUTF8  = "utf8"
)

// charsetConn is a Conn that, in UTF-8 mode, writes the CP437 it's given
// as UTF-8 and reads UTF-8 as CP437.
type charsetConn struct {
	Conn
	utf8 bool
	// partial is the start of a UTF-8 character whose rest hasn't
	// arrived yet; out is what was read and turned into CP437 but
	// didn't fit into the caller's buffer.
	partial []byte
	out     []byte
	// bg is a read still under way that readFor gave up waiting for;
	// the next Read takes its result before reading anything else.
	bg chan bgRead
}

type bgRead struct {
	data []byte
	err  error
}

func (c *charsetConn) Write(p []byte) (int, error) {
	if !c.utf8 {
		return c.Conn.Write(p)
	}
	if _, err := c.Conn.Write([]byte(ansi.DecodeCP437(p))); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *charsetConn) Read(p []byte) (int, error) {
	for len(c.out) == 0 {
		var raw []byte
		if c.bg != nil {
			r := <-c.bg
			c.bg = nil
			if r.err != nil && len(r.data) == 0 {
				return 0, r.err
			}
			raw = r.data
		} else if !c.utf8 {
			return c.Conn.Read(p)
		} else {
			// No more than asked for: what isn't read yet stays with
			// the connection, for a file transfer taking over (Binary).
			buf := make([]byte, len(p))
			n, err := c.Conn.Read(buf)
			if n == 0 {
				return 0, err
			}
			raw = buf[:n]
		}
		c.out = append(c.out, c.decode(raw)...)
	}
	n := copy(p, c.out)
	c.out = c.out[n:]
	return n, nil
}

// decode is raw as the board reads it: in UTF-8 mode each character as
// its CP437 byte (or a stand-in, see ansi.EncodeCP437); a byte that
// isn't UTF-8 after all as it is.
func (c *charsetConn) decode(raw []byte) []byte {
	if !c.utf8 {
		return raw
	}
	b := append(c.partial, raw...)
	c.partial = nil
	var out []byte
	for len(b) > 0 {
		if b[0] < 0x80 {
			out = append(out, b[0])
			b = b[1:]
			continue
		}
		if !utf8.FullRune(b) {
			c.partial = append([]byte(nil), b...)
			break
		}
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size == 1 {
			out = append(out, b[0])
		} else {
			out = append(out, ansi.EncodeCP437(string(r))...)
		}
		b = b[size:]
	}
	return out
}

// buffered is how many bytes are there to read without waiting, and
// whether that's known (see Terminal.ReadKey's Escape key).
func (c *charsetConn) buffered() (int, bool) {
	if len(c.out) > 0 {
		return len(c.out), true
	}
	if c.bg != nil {
		return 0, false
	}
	b, ok := c.Conn.(interface{ Buffered() int })
	if !ok {
		return 0, false
	}
	return b.Buffered(), true
}

// readFor reads what arrives within d -- for a terminal's answer that
// may never come. A read that's still waiting then is left for the
// next Read.
func (c *charsetConn) readFor(d time.Duration) ([]byte, error) {
	if len(c.out) > 0 {
		b := c.out
		c.out = nil
		return b, nil
	}
	if c.bg == nil {
		ch := make(chan bgRead, 1)
		c.bg = ch
		go func() {
			buf := make([]byte, 256)
			n, err := c.Conn.Read(buf)
			ch <- bgRead{buf[:n], err}
		}()
	}
	select {
	case r := <-c.bg:
		c.bg = nil
		return c.decode(r.data), r.err
	case <-time.After(d):
		return nil, nil
	}
}

// charsetWait is how long a terminal has to answer detectUTF8 -- one
// that answers does at once; one that doesn't costs this at connect.
const charsetWait = 1500 * time.Millisecond

// cprReply is a terminal's cursor position report: ESC [ row ; col R.
var cprReply = regexp.MustCompile(`\x1b\[(\d+);(\d+)R`)

// cprProbe is "é" in UTF-8 at the start of the line, then the question
// where the cursor is: a UTF-8 terminal shows one character (column
// 2), a CP437 one two (column 3: "├⌐").
const cprProbe = "\r\xc3\xa9\x1b[6n"

// detectUTF8 asks the terminal whether it speaks UTF-8 and wipes the
// probe off the screen again. ok is false when it didn't answer within
// wait (a terminal that doesn't do cursor reports): CP437 then, as
// always. Whatever the caller typed meanwhile is kept for reading.
func (t *Terminal) detectUTF8(wait time.Duration) (isUTF8, ok bool) {
	c := t.cs
	if _, err := c.Conn.Write([]byte(cprProbe)); err != nil {
		return false, false
	}
	defer c.Conn.Write([]byte("\r\x1b[K"))
	var got []byte
	deadline := time.Now().Add(wait)
	for {
		if m := cprReply.FindSubmatchIndex(got); m != nil {
			col, _ := strconv.Atoi(string(got[m[4]:m[5]]))
			rest := append(append([]byte(nil), got[:m[0]]...), got[m[1]:]...)
			c.out = append(rest, c.out...)
			return col == 2, col == 2 || col == 3
		}
		left := time.Until(deadline)
		if left <= 0 {
			break
		}
		b, err := c.readFor(left)
		got = append(got, b...)
		if err != nil {
			break
		}
	}
	// No answer: what did come is input.
	if i := bytes.Index(got, []byte("\x1b[")); i >= 0 && !bytes.ContainsAny(got[i:], "R") {
		got = got[:i]
	}
	c.out = append(got, c.out...)
	return false, false
}
