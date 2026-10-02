package bbs

import (
	"time"

	"git.maik.ch/nullmodem/kit/ansi"
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
	conn Conn
	// Node is the node number this caller is connected on (0 until
	// the session assigns one), for things that need it outside the
	// menu loop -- e.g. a door's drop file.
	Node int
	// RemoteIP and Protocol ("telnet", "ssh") are the caller's, for
	// recording failed logins (internal/guard).
	RemoteIP string
	Protocol string
	// sysopOK is set once the caller passed the two-factor check for
	// the sysop functions (sysopGate) this session.
	sysopOK bool
	// pending holds at most one byte pushed back by ReadKey's escape-
	// sequence lookahead (an 0x1b not followed by '[').
	pending []byte
	// expectLFOrNUL is set right after a line-terminating CR is
	// consumed (see readByte/ReadLine) and checked on the very next
	// byte read, wherever that ends up happening -- swallowing it
	// silently if it turns out to be the LF or NUL some clients pair
	// with CR (RFC 854's "CR LF"/"CR NUL"), without ever blocking to
	// wait for one. An earlier version blocked reading one more byte
	// right after every CR specifically to check for this, live-tested
	// against a real client (SyncTERM) whose Enter key sends a bare CR
	// with no companion byte at all: that peek then blocked until the
	// *next* keypress arrived, needing Enter pressed twice (the first
	// press to reach the peek and the second to finally satisfy it)
	// with a long visible stall in between the two.
	expectLFOrNUL bool
	// loc is the zone times are shown in for this session -- the
	// caller's profile time zone once logged in (see SetLocation), UTC
	// until then.
	loc *time.Location
	// threadView: the message lists show threads (T), for this call.
	threadView bool
}

// SetLocation sets the zone Time converts to, e.g. after login or when
// the caller changes their profile's time zone.
func (t *Terminal) SetLocation(loc *time.Location) { t.loc = loc }

// Time converts tm to this session's display zone (UTC if none is set).
// Format it with an "MST" zone abbreviation wherever there's room, so
// a caller can tell which zone they're looking at.
func (t *Terminal) Time(tm time.Time) time.Time {
	if t.loc == nil {
		return tm.UTC()
	}
	return tm.In(t.loc)
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
// hotkey dispatch, e.g. "download this file"): t.pending can only hold
// a byte pushed back by ReadKey's own escape-sequence lookahead, and
// t.expectLFOrNUL only ever causes a byte already sitting on the wire
// to be silently dropped rather than holding one back, so there's
// nothing already consumed left to lose. See PushBack for handing
// bytes the raw caller itself consumed but couldn't use back to this
// Terminal once it's done.
func (t *Terminal) Raw() Conn { return t.conn }

// PushBack makes b the next bytes readByte (and so ReadKey/ReadLine)
// returns, ahead of anything still unread on the wire. For a Raw
// caller (internal/zmodem's downloadFile) that ends up reading a byte
// off the connection it can't actually use for itself -- e.g. the
// caller's own next keystroke, arriving the instant an external sz
// subprocess exits and stops wanting input -- so it isn't silently
// lost. Appends to, rather than replacing, whatever's already
// pending, though in practice this is only ever called with pending
// empty (Raw's own doc comment explains why).
func (t *Terminal) PushBack(b []byte) {
	t.pending = append(t.pending, b...)
}

// readByte returns the next input byte: first draining any bytes
// pushed back (see PushBack; also used by ReadKey's own escape-
// sequence lookahead), then honoring a pending expectLFOrNUL by
// silently dropping exactly one LF or NUL (see its doc comment)
// before returning the byte after it.
func (t *Terminal) readByte() (byte, error) {
	if len(t.pending) > 0 {
		b := t.pending[0]
		t.pending = t.pending[1:]
		return b, nil
	}
	buf := make([]byte, 1)
	for {
		n, err := t.conn.Read(buf)
		if err != nil {
			return 0, err
		}
		if n == 0 {
			continue
		}
		b := buf[0]
		if t.expectLFOrNUL {
			t.expectLFOrNUL = false
			if b == charLF || b == 0 {
				continue
			}
		}
		return b, nil
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
	KeyHome
	KeyEnd
	KeyDelete
	KeyPgUp
	KeyPgDn
	// KeyCtrl is a control key, Ctrl-A to Ctrl-Z: Rune holds its
	// letter ('a'..'z'). Tab comes as Ctrl-I.
	KeyCtrl
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
			t.expectLFOrNUL = true
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
		// "ESC [ n ~" (Home 1/7, Delete 3, End 4/8, PgUp 5, PgDn 6):
		// digits, then the tilde.
		var num int
		for final >= '0' && final <= '9' {
			num = num*10 + int(final-'0')
			if final, err = t.readByte(); err != nil {
				return Key{}, err
			}
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
		case 'H':
			return Key{Type: KeyHome}, nil
		case 'F', 'K':
			return Key{Type: KeyEnd}, nil
		case '~':
			switch num {
			case 1, 7:
				return Key{Type: KeyHome}, nil
			case 3:
				return Key{Type: KeyDelete}, nil
			case 4, 8:
				return Key{Type: KeyEnd}, nil
			case 5:
				return Key{Type: KeyPgUp}, nil
			case 6:
				return Key{Type: KeyPgDn}, nil
			}
			return Key{Type: KeyUnknown}, nil
		default:
			return Key{Type: KeyUnknown}, nil
		}

	case c >= 0x20 && c <= 0xfe:
		return Key{Type: KeyChar, Rune: rune(c)}, nil

	case c >= 0x01 && c <= 0x1a:
		return Key{Type: KeyCtrl, Rune: rune('a' + c - 1)}, nil

	default:
		return Key{Type: KeyUnknown}, nil
	}
}

// ReadLine reads one line of input, echoing typed characters (masked
// as '*' when mask is true) and honoring backspace/delete for editing.
// It returns the line without its trailing CR/LF. Both bare LF and
// CRLF line endings are accepted; a line-ending CR returns immediately
// (see expectLFOrNUL) rather than blocking to check whether a
// companion LF/NUL follows -- a following one, whenever it does
// arrive, is silently dropped instead.
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
				t.expectLFOrNUL = true
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
