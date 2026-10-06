package doors

import (
	"io"
	"strconv"
	"strings"
)

// outputFilter adjusts a door's output on its way to the caller:
//
//   - crlf turns a bare LF into CR LF. A door on standard I/O (Door.Stdio)
//     writes lines the Unix way, trusting a terminal driver to add the
//     CR; there's none between it and the caller here, so every line
//     would otherwise start where the previous one ended.
//   - ansi16 rewrites every SGR sequence into the classic form BBS
//     terminals (ANSI.SYS and its heirs) understand: the 256-colour and
//     true-colour ones and the aixterm bright colours 90-97/100-107 go
//     to the nearest of the 16 classic colours -- such a terminal would
//     read "38;5;130" as three separate attributes, 5 being blink -- and
//     since it doesn't know 39/49 (default colours) or 22 (normal
//     intensity) either, the filter keeps the colour state and sends all
//     of it each time: "ESC[0;1;33;44m". A door's "back to the default
//     background" otherwise left the last one painting the rest of the
//     line, stripes across its art.
//     Nor do they know "ESC[?7l" (no automatic wrap): a door drawing
//     full 80-column lines under it got a wrap after each one there,
//     plus its own line break -- every other line empty, painted in the
//     colour at the right edge. So while a door has wrapping off, the
//     filter keeps the cursor column and never writes the last one.
//
// Escape sequences split across reads are held back until complete.
type outputFilter struct {
	r      io.Reader
	crlf   bool
	ansi16 bool

	// The colour state the caller's terminal is in (ansi16): classic
	// colours 0-7, bold being bright.
	fg, bg                int
	bold, blink, reversed bool
	// The cursor column (0-based), and whether the door turned the
	// terminal's automatic wrap off (ansi16).
	col    int
	noWrap bool

	lastCR  bool   // the previous byte passed on was CR
	esc     []byte // an escape sequence still being collected
	pending []byte // filtered output not yet returned
}

// classicWidth is the width of the classic terminal ansi16 is for.
const classicWidth = 80

// maxEscape bounds a held-back escape sequence; anything longer isn't
// one we rewrite and is passed on as it is.
const maxEscape = 64

func newOutputFilter(r io.Reader, crlf, ansi16 bool) io.Reader {
	if !crlf && !ansi16 {
		return r
	}
	return &outputFilter{r: r, crlf: crlf, ansi16: ansi16, fg: 7}
}

func (f *outputFilter) Read(p []byte) (int, error) {
	for len(f.pending) == 0 {
		buf := make([]byte, len(p))
		n, err := f.r.Read(buf)
		f.pending = f.filter(buf[:n])
		if err != nil {
			if len(f.esc) > 0 { // the stream ended mid-sequence
				f.pending = append(f.pending, f.esc...)
				f.esc = nil
			}
			if len(f.pending) == 0 {
				return 0, err
			}
			break
		}
	}
	n := copy(p, f.pending)
	f.pending = f.pending[n:]
	return n, nil
}

func (f *outputFilter) filter(in []byte) []byte {
	out := make([]byte, 0, len(in)+len(in)/8)
	for _, b := range in {
		if f.ansi16 && (len(f.esc) > 0 || b == 0x1b) {
			f.esc = append(f.esc, b)
			if done, seq := f.escapeDone(); done {
				out = append(out, seq...)
				f.esc = nil
			}
			continue
		}
		if f.ansi16 && !f.track(b) {
			continue
		}
		if f.crlf && b == '\n' && !f.lastCR {
			out = append(out, '\r')
			f.col = 0
		}
		out = append(out, b)
		f.lastCR = b == '\r'
	}
	return out
}

// track moves the cursor column for b and reports whether b is to be
// passed on: a character in the last column isn't while the door has
// wrapping off.
func (f *outputFilter) track(b byte) bool {
	switch {
	case b == '\r':
		f.col = 0
	case b == '\b':
		f.col = max(0, f.col-1)
	case b == '\t':
		f.col = min(classicWidth-1, (f.col/8+1)*8)
	case b >= 0x20 && b != 0x7f:
		if f.noWrap && f.col >= classicWidth-1 {
			return false
		}
		f.col++
	}
	return true
}

// cursor follows a CSI sequence's effect on the cursor column, and
// takes "?7l"/"?7h" (wrap off/on) for itself: the classic terminal
// doesn't know them, the filter does it in their place.
func (f *outputFilter) cursor(params []byte, final byte) (keep bool) {
	p := string(params)
	if p == "?7" && (final == 'l' || final == 'h') {
		f.noWrap = final == 'l'
		return false
	}
	arg := func(i, def int) int {
		parts := strings.Split(p, ";")
		if i < len(parts) {
			if n, err := strconv.Atoi(parts[i]); err == nil && n > 0 {
				return n
			}
		}
		return def
	}
	switch final {
	case 'H', 'f':
		f.col = arg(1, 1) - 1
	case 'G':
		f.col = arg(0, 1) - 1
	case 'C':
		f.col = min(classicWidth-1, f.col+arg(0, 1))
	case 'D':
		f.col = max(0, f.col-arg(0, 1))
	}
	return true
}

// escapeDone reports whether f.esc holds a finished escape sequence,
// and what to pass on for it.
func (f *outputFilter) escapeDone() (bool, []byte) {
	e := f.esc
	if len(e) == 1 {
		return false, nil
	}
	if e[1] != '[' || len(e) > maxEscape {
		return true, e // not a CSI sequence, or too long: untouched
	}
	last := e[len(e)-1]
	if len(e) == 2 || last < 0x40 || last > 0x7e {
		return false, nil
	}
	if last != 'm' {
		if !f.cursor(e[2:len(e)-1], last) {
			return true, nil
		}
		return true, e
	}
	return true, f.rewriteSGR(e[2 : len(e)-1])
}

// rewriteSGR applies params (between "ESC[" and "m") to the colour
// state and returns the classic SGR sequence for the whole of it.
func (f *outputFilter) rewriteSGR(params []byte) []byte {
	parts := strings.Split(strings.ReplaceAll(string(params), ":", ";"), ";")
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil && p != "" {
			// Not plain numbers: leave the sequence alone.
			return append(append([]byte("\x1b["), params...), 'm')
		}
		nums[i] = n
	}
	setFG := func(c int) { f.fg, f.bold = c%8, c >= 8 }
	for i := 0; i < len(nums); i++ {
		switch n := nums[i]; {
		case n == 0:
			f.fg, f.bg, f.bold, f.blink, f.reversed = 7, 0, false, false, false
		case n == 1:
			f.bold = true
		case n == 22:
			f.bold = false
		case n == 5 || n == 6:
			f.blink = true
		case n == 25:
			f.blink = false
		case n == 7:
			f.reversed = true
		case n == 27:
			f.reversed = false
		case n >= 30 && n <= 37:
			f.fg = n - 30
		case n == 39:
			f.fg = 7
		case n >= 40 && n <= 47:
			f.bg = n - 40
		case n == 49:
			f.bg = 0
		case n >= 90 && n <= 97:
			setFG(n - 90 + 8)
		case n >= 100 && n <= 107:
			f.bg = n - 100
		case (n == 38 || n == 48) && i+2 < len(nums) && nums[i+1] == 5:
			if c := nearestANSI(xterm256(nums[i+2]), n == 48); n == 48 {
				f.bg = c
			} else {
				setFG(c)
			}
			i += 2
		case (n == 38 || n == 48) && i+4 < len(nums) && nums[i+1] == 2:
			if c := nearestANSI([3]int{nums[i+2], nums[i+3], nums[i+4]}, n == 48); n == 48 {
				f.bg = c
			} else {
				setFG(c)
			}
			i += 4
		}
		// Anything else (underline, italics, ...) a BBS terminal
		// doesn't have: dropped.
	}
	out := "\x1b[0"
	if f.bold {
		out += ";1"
	}
	if f.blink {
		out += ";5"
	}
	if f.reversed {
		out += ";7"
	}
	return []byte(out + ";" + strconv.Itoa(30+f.fg) + ";" + strconv.Itoa(40+f.bg) + "m")
}

// ansiPalette is the 16 classic colours as a BBS terminal shows them
// (the VGA text-mode palette), in ANSI order.
var ansiPalette = [16][3]int{
	{0x00, 0x00, 0x00}, {0xaa, 0x00, 0x00}, {0x00, 0xaa, 0x00}, {0xaa, 0x55, 0x00},
	{0x00, 0x00, 0xaa}, {0xaa, 0x00, 0xaa}, {0x00, 0xaa, 0xaa}, {0xaa, 0xaa, 0xaa},
	{0x55, 0x55, 0x55}, {0xff, 0x55, 0x55}, {0x55, 0xff, 0x55}, {0xff, 0xff, 0x55},
	{0x55, 0x55, 0xff}, {0xff, 0x55, 0xff}, {0x55, 0xff, 0xff}, {0xff, 0xff, 0xff},
}

// nearestANSI maps rgb to an ANSI colour by hue and brightness rather
// than raw distance -- the VGA palette's bright colours are washed out
// (FF5555), so plain distance would turn a vivid red into dark red.
// Greys go by lightness. A background only has the first eight.
func nearestANSI(rgb [3]int, background bool) int {
	r, g, b := rgb[0], rgb[1], rgb[2]
	hi, lo := max(r, g, b), min(r, g, b)
	if hi-lo < 0x30 { // grey
		switch {
		case background && hi < 0x80:
			return 0
		case background:
			return 7
		case hi < 0x40:
			return 0
		case hi < 0x80:
			return 8
		case hi < 0xe0:
			return 7
		default:
			return 15
		}
	}
	c := 0
	if r*2 >= hi {
		c |= 1
	}
	if g*2 >= hi {
		c |= 2
	}
	if b*2 >= hi {
		c |= 4
	}
	if !background && hi >= 0xd0 {
		c += 8
	}
	return c
}

// xterm256 is the RGB value of xterm colour n.
func xterm256(n int) [3]int {
	switch {
	case n < 0 || n > 255:
		return [3]int{0, 0, 0}
	case n < 16:
		return ansiPalette[n]
	case n < 232:
		n -= 16
		level := func(v int) int {
			if v == 0 {
				return 0
			}
			return 55 + 40*v
		}
		return [3]int{level(n / 36), level(n / 6 % 6), level(n % 6)}
	default:
		g := 8 + 10*(n-232)
		return [3]int{g, g, g}
	}
}
