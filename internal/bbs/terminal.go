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

// maxWidth caps the NAWS width Width() will trust: classic 80-column
// BBS art, which every terminal genuinely handles. Going higher isn't
// safe to act on -- in real testing, SyncTERM reported a NAWS width
// larger than what it actually rendered without wrapping after its
// window was resized. Genuinely narrower clients (e.g. old 40-column
// terminals) are still respected, since only the upper bound is
// capped.
const maxWidth = 80

// wrapMargin is subtracted from the (capped) width so content never
// touches the very last column. Also found in real testing: even at
// exactly 80 columns -- a width every terminal is supposed to
// handle -- SyncTERM still wrapped a full-width line. Writing to a
// terminal's final column and continuing is a well-known auto-wrap
// edge case (the cursor's pending-wrap state fires on the next write
// rather than exactly at the margin), so the last column is left
// deliberately unused rather than chasing exact per-terminal behavior.
const wrapMargin = 1

// Width returns the safe column count to lay screens and menus out to
// (see ansi.Layout): the client's negotiated NAWS width, capped at
// maxWidth and falling back to it if the client hasn't reported one,
// minus wrapMargin.
func (t *Terminal) Width() int {
	w, _ := t.conn.WindowSize()
	if w <= 0 || w > maxWidth {
		w = maxWidth
	}
	w -= wrapMargin
	if w < 1 {
		w = 1
	}
	return w
}

// defaultHeight is what Height() reports when the client hasn't
// negotiated one (or reports something implausible) -- the same
// classic-BBS row count internal/telnet.Session already falls back to
// before any NAWS update arrives.
const defaultHeight = 24

// Height returns the client's negotiated NAWS row count, falling back
// to defaultHeight if the client hasn't reported one or reports
// something nonsensical (zero/negative). Unlike Width, there's no
// known upper cap to enforce -- a tall terminal window is just a
// tall terminal window.
func (t *Terminal) Height() int {
	_, h := t.conn.WindowSize()
	if h <= 0 {
		h = defaultHeight
	}
	return h
}

// Print writes s to the client, translating bare LF to CRLF.
func (t *Terminal) Print(s string) error {
	_, err := t.conn.Write([]byte(ansi.ToCRLF(s)))
	return err
}

// PrintRaw writes s to the client completely unchanged -- no bare-LF-
// to-CRLF translation (see Print). For content whose exact bytes were
// chosen deliberately and must reach the terminal as authored, like
// pre-formatted ANSI art: real .ANS art commonly pairs a bare LF with
// cursor save/restore (ESC[s ... ESC[u) to advance one row while
// snapping back to a remembered column, and Print's automatic \r
// insertion shifts terminal state the artist never intended,
// scrambling the result (confirmed live against a real fsxNet ad).
func (t *Terminal) PrintRaw(s string) error {
	_, err := t.conn.Write([]byte(s))
	return err
}

// Println writes s followed by a newline.
func (t *Terminal) Println(s string) error {
	return t.Print(s + "\n")
}

// Raw returns the underlying connection for a caller that needs to
// take over the byte stream directly -- internal/zmodem's file
// transfers, which are an 8-bit binary protocol with nothing to do
// with this Terminal's own line-oriented/ANSI-cooked interaction, so
// bypassing it (rather than teaching Terminal to speak Zmodem itself)
// is the right layering. Safe to call between key reads (a menu
// hotkey dispatch, e.g. "download this file"): t.pending can only
// hold a byte pushed back by ReadLine's own CRLF lookahead, never by
// ReadKey, so it's empty at that point and there's nothing already
// consumed from the wire left to lose.
func (t *Terminal) Raw() Conn { return t.conn }

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

// KeyType classifies one keypress read by ReadKey.
type KeyType int

const (
	KeyChar KeyType = iota
	KeyEnter
	KeyBackspace
	KeyEscape
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyUnknown
)

// Key is one keypress as read by ReadKey: for KeyChar, Rune holds the
// printable character; every other Type ignores it.
type Key struct {
	Type KeyType
	Rune rune
}

// ReadKey reads a single keypress, unlike ReadLine's line-buffered
// input: no local echo, and arrow keys are recognized (as the
// standard VT100/ANSI cursor-key CSI sequences real BBS terminal
// clients send: ESC [ A/B/C/D) rather than being swallowed as
// unrecognized control bytes. Intended for lightbar-style UIs that
// redraw themselves after every key rather than editing a line of
// text.
func (t *Terminal) ReadKey() (Key, error) {
	c, err := t.readByte()
	if err != nil {
		return Key{}, err
	}
	switch {
	case c == charCR || c == charLF:
		if c == charCR {
			if next, err := t.readByte(); err == nil && next != charLF {
				t.pending = []byte{next}
			}
		}
		return Key{Type: KeyEnter}, nil

	case c == charBackspace || c == charDelete:
		return Key{Type: KeyBackspace}, nil

	case c == 0x1b:
		next, err := t.readByte()
		if err != nil {
			// Nothing followed the escape byte before the connection
			// ended; report it as a bare Escape rather than losing it.
			return Key{Type: KeyEscape}, nil
		}
		if next != '[' {
			t.pending = []byte{next}
			return Key{Type: KeyEscape}, nil
		}
		final, err := t.readByte()
		if err != nil {
			return Key{}, err
		}
		switch final {
		case 'A':
			return Key{Type: KeyUp}, nil
		case 'B':
			return Key{Type: KeyDown}, nil
		case 'C':
			return Key{Type: KeyRight}, nil
		case 'D':
			return Key{Type: KeyLeft}, nil
		default:
			return Key{Type: KeyUnknown}, nil
		}

	case c >= 0x20 && c <= 0xfe:
		return Key{Type: KeyChar, Rune: rune(c)}, nil

	default:
		return Key{Type: KeyUnknown}, nil
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
