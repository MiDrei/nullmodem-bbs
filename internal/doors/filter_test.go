package doors

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func filtered(t *testing.T, in string, crlf, ansi16 bool) string {
	t.Helper()
	// OneByteReader splits every escape sequence across reads.
	out, err := io.ReadAll(newOutputFilter(iotest.OneByteReader(bytes.NewReader([]byte(in))), crlf, ansi16))
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestOutputFilterAddsCRToBareLF(t *testing.T) {
	got := filtered(t, "one\ntwo\r\nthree\n\n", true, false)
	if want := "one\r\ntwo\r\nthree\r\n\r\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestOutputFilterReduces256AndTrueColour(t *testing.T) {
	for in, want := range map[string]string{
		"\x1b[38;5;196mX":           "\x1b[0;1;31;40mX",     // bright red
		"\x1b[48;5;130mX":           "\x1b[0;37;43mX",       // brown background
		"\x1b[0;38;5;252;48;5;16mX": "\x1b[0;37;40mX",       // light grey on black
		"\x1b[38;2;0;0;170mX":       "\x1b[0;34;40mX",       // true colour blue
		"\x1b[38:5:46mX":            "\x1b[0;1;32;40mX",     // colon form
		"\x1b[93;104mX":             "\x1b[0;1;33;44mX",     // aixterm bright
		"\x1b[1;31mX":               "\x1b[0;1;31;40mX",     // classic, as a whole state
		"\x1b[2J\x1b[10;5HX":        "\x1b[2J\x1b[10;5HX",   // not SGR: untouched
		"\xdb\xdf\xff\xfb\x01":      "\xdb\xdf\xff\xfb\x01", // CP437 and telnet bytes pass
	} {
		if got := filtered(t, in, false, true); got != want {
			t.Errorf("filter(%q) = %q, want %q", in, got, want)
		}
	}
}

// The codes a classic terminal ignores -- default background 49 and
// foreground 39, normal intensity 22 -- come out as the colour state
// they leave, so the last background doesn't paint on.
func TestOutputFilterKeepsTheStateForClassicTerminals(t *testing.T) {
	in := "\x1b[48;5;30mA\x1b[49mB\x1b[38;5;226mC\x1b[22mD\x1b[39mE\x1b[1;44mF\x1b[38;5;0mG"
	want := "\x1b[0;37;46mA\x1b[0;37;40mB\x1b[0;1;33;40mC\x1b[0;33;40mD\x1b[0;37;40mE\x1b[0;1;37;44mF\x1b[0;30;44mG"
	if got := filtered(t, in, false, true); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestOutputFilterPassesAnUnfinishedSequenceAtEOF(t *testing.T) {
	if got := filtered(t, "ok\x1b[38;5", false, true); got != "ok\x1b[38;5" {
		t.Fatalf("got %q", got)
	}
}

func TestOutputFilterOffIsTheReaderItself(t *testing.T) {
	r := bytes.NewReader(nil)
	if newOutputFilter(r, false, false) != io.Reader(r) {
		t.Fatal("a filter with nothing to do should not wrap the reader")
	}
}

// With wrapping off ("?7l", which classic terminals don't know) the
// last column is never written, so such a terminal doesn't wrap after
// a full line; with it on, long lines pass for the terminal to wrap.
func TestOutputFilterKeepsOffTheLastColumnWithoutWrap(t *testing.T) {
	full := strings.Repeat("A", 80)
	if got, want := filtered(t, "\x1b[?7l"+full+"\r\n\x1b[1;75HBCDEFG\x1b[?7h"+full+"B", false, true),
		strings.Repeat("A", 79)+"\r\n\x1b[1;75HBCDEF"+full+"B"; got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestLineEndDropperDropsOnlyAFirstLFOrNUL(t *testing.T) {
	for in, want := range map[string]string{"\nabc\n": "abc\n", "\x00x": "x", "abc\n": "abc\n", "\r\n": "\r\n"} {
		out, _ := io.ReadAll(&lineEndDropper{r: iotest.OneByteReader(strings.NewReader(in))})
		if string(out) != want {
			t.Errorf("%q -> %q, want %q", in, out, want)
		}
	}
}
