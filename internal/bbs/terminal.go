package bbs

import (
	"git.maik.ch/swissmaik/nullmodem/internal/ansi"
)

const (
	charBackspace = 0x08
	charDelete    = 0x7f
	charCR        = '\r'
	charLF        = '\n'
)

// Terminal wraps a Conn with line-oriented I/O and manual echo, which
// both the telnet and SSH transports require once local client echo is
// suppressed.
type Terminal struct {
	conn    Conn
	pending []byte // at most one byte pushed back by CRLF lookahead
}

// NewTerminal wraps conn for line-based interaction.
func NewTerminal(conn Conn) *Terminal { return &Terminal{conn: conn} }

// defaultWidth is used when a client never reports a window size
// (e.g. NAWS wasn't negotiated), matching classic 80-column BBS art.
const defaultWidth = 80

// Width returns the client's negotiated terminal width, for laying
// out screens and menus (see ansi.Layout), falling back to
// defaultWidth if the client hasn't reported one.
func (t *Terminal) Width() int {
	w, _ := t.conn.WindowSize()
	if w <= 0 {
		return defaultWidth
	}
	return w
}

// Print writes s to the client, translating bare LF to CRLF.
func (t *Terminal) Print(s string) error {
	_, err := t.conn.Write([]byte(ansi.ToCRLF(s)))
	return err
}

// Println writes s followed by a newline.
func (t *Terminal) Println(s string) error {
	return t.Print(s + "\n")
}

// readByte returns the next input byte, first draining any byte
// pushed back by a previous CRLF lookahead.
func (t *Terminal) readByte() (byte, error) {
	if len(t.pending) > 0 {
		b := t.pending[0]
		t.pending = nil
		return b, nil
	}
	buf := make([]byte, 1)
	for {
		n, err := t.conn.Read(buf)
		if err != nil {
			return 0, err
		}
		if n > 0 {
			return buf[0], nil
		}
	}
}

// ReadLine reads one line of input, echoing typed characters (masked
// as '*' when mask is true) and honoring backspace/delete for editing.
// It returns the line without its trailing CR/LF. Both bare LF and
// CRLF line endings are accepted; when a line ends in CR, a following
// LF is consumed as part of the same terminator rather than leaking
// into the next ReadLine call as a spurious blank line.
func (t *Terminal) ReadLine(mask bool) (string, error) {
	var line []byte

	for {
		c, err := t.readByte()
		if err != nil {
			return "", err
		}

		switch {
		case c == charCR || c == charLF:
			if c == charCR {
				if next, err := t.readByte(); err == nil && next != charLF {
					t.pending = []byte{next}
				}
			}
			if _, err := t.conn.Write([]byte(ansi.CRLF)); err != nil {
				return "", err
			}
			return string(line), nil

		case c == charBackspace || c == charDelete:
			if len(line) > 0 {
				line = line[:len(line)-1]
				if _, err := t.conn.Write([]byte{charBackspace, ' ', charBackspace}); err != nil {
					return "", err
				}
			}

		case c >= 0x20 && c <= 0xfe:
			line = append(line, c)
			echo := c
			if mask {
				echo = '*'
			}
			if _, err := t.conn.Write([]byte{echo}); err != nil {
				return "", err
			}

		default:
			// Ignore other control bytes (arrow key escape sequences,
			// tabs, etc.) for this minimal line editor.
		}
	}
}
