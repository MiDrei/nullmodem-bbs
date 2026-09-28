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
//   - ansi16 rewrites 256-colour and true-colour SGR sequences (and the
//     aixterm bright colours 90-97/100-107) into the 16 classic ANSI
//     colours, for BBS terminals that only have those -- they'd read
//     "38;5;130" as three separate attributes, 5 being blink.
//
// Escape sequences split across reads are held back until complete.
type outputFilter struct {
	r      io.Reader
	crlf   bool
	ansi16 bool

	lastCR  bool   // the previous byte passed on was CR
	esc     []byte // an escape sequence still being collected
	pending []byte // filtered output not yet returned
}

// maxEscape bounds a held-back escape sequence; anything longer isn't
// one we rewrite and is passed on as it is.
const maxEscape = 64

func newOutputFilter(r io.Reader, crlf, ansi16 bool) io.Reader {
	if !crlf && !ansi16 {
		return r
	}
	return &outputFilter{r: r, crlf: crlf, ansi16: ansi16}
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
		if f.crlf && b == '\n' && !f.lastCR {
			out = append(out, '\r')
		}
		out = append(out, b)
		f.lastCR = b == '\r'
	}
	return out
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
		return true, e
	}
	return true, rewriteSGR(e[2 : len(e)-1])
}

// rewriteSGR returns the SGR sequence for params (between "ESC[" and
// "m") with every colour beyond the 16 classic ones replaced by the
// nearest of those.
func rewriteSGR(params []byte) []byte {
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
	var out []string
	for i := 0; i < len(nums); i++ {
		n := nums[i]
		switch {
		case (n == 38 || n == 48) && i+2 < len(nums) && nums[i+1] == 5:
			out = append(out, colorSGR(n == 48, nearestANSI(xterm256(nums[i+2]), n == 48)))
			i += 2
		case (n == 38 || n == 48) && i+4 < len(nums) && nums[i+1] == 2:
			out = append(out, colorSGR(n == 48, nearestANSI([3]int{nums[i+2], nums[i+3], nums[i+4]}, n == 48)))
			i += 4
		case n >= 90 && n <= 97:
			out = append(out, colorSGR(false, n-90+8))
		case n >= 100 && n <= 107:
			out = append(out, colorSGR(true, n-100))
		default:
			out = append(out, parts[i])
		}
	}
	return []byte("\x1b[" + strings.Join(out, ";") + "m")
}

// colorSGR is the classic SGR for ANSI colour c (0-15): bright
// foregrounds as bold, backgrounds from the eight non-bright ones.
func colorSGR(background bool, c int) string {
	if background {
		return strconv.Itoa(40 + c%8)
	}
	if c >= 8 {
		return "1;" + strconv.Itoa(30+c-8)
	}
	return "22;" + strconv.Itoa(30+c)
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
