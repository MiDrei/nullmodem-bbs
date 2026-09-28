package doors

import (
	"bytes"
	"io"
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
		"\x1b[38;5;196mX":           "\x1b[1;31mX",          // bright red
		"\x1b[48;5;130mX":           "\x1b[43mX",            // brown background
		"\x1b[0;38;5;252;48;5;16mX": "\x1b[0;22;37;40mX",    // light grey on black
		"\x1b[38;2;0;0;170mX":       "\x1b[22;34mX",         // true colour blue
		"\x1b[38:5:46mX":            "\x1b[1;32mX",          // colon form
		"\x1b[93;104mX":             "\x1b[1;33;44mX",       // aixterm bright
		"\x1b[1;31mX":               "\x1b[1;31mX",          // classic: untouched
		"\x1b[2J\x1b[10;5HX":        "\x1b[2J\x1b[10;5HX",   // not SGR: untouched
		"\xdb\xdf\xff\xfb\x01":      "\xdb\xdf\xff\xfb\x01", // CP437 and telnet bytes pass
	} {
		if got := filtered(t, in, false, true); got != want {
			t.Errorf("filter(%q) = %q, want %q", in, got, want)
		}
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
