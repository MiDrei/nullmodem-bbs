package ansi

import (
	"strings"
	"testing"
)

func TestToHTMLPlainTextHasNoSpanForDefaultColors(t *testing.T) {
	got := ToHTML("hi")
	// bg 0 (black, the CP437 default) is omitted -- see openSpan's own
	// doc comment for why: it's meant to mean "whatever the caller's
	// own background already is," not literally force black.
	want := `<span style="color:#AAAAAA">hi</span>`
	if got != want {
		t.Fatalf("ToHTML(%q) = %q, want %q", "hi", got, want)
	}
}

func TestToHTMLAppliesBrightForegroundColor(t *testing.T) {
	got := ToHTML("\x1b[1;36mhi\x1b[0m")
	want := `<span style="color:#55FFFF">hi</span>`
	if got != want {
		t.Fatalf("ToHTML = %q, want %q", got, want)
	}
}

func TestToHTMLBrightAppliesRegardlessOfParamOrder(t *testing.T) {
	a := ToHTML("\x1b[1;36mX")
	b := ToHTML("\x1b[36;1mX")
	if a != b {
		t.Fatalf("param order changed output: %q vs %q", a, b)
	}
	if a != `<span style="color:#55FFFF">X</span>` {
		t.Fatalf("unexpected bright cyan rendering: %q", a)
	}
}

func TestToHTMLHandlesBackgroundAndReset(t *testing.T) {
	got := ToHTML("\x1b[44mA\x1b[0mB")
	want := `<span style="color:#AAAAAA;background-color:#0000AA">A</span>` +
		`<span style="color:#AAAAAA">B</span>`
	if got != want {
		t.Fatalf("ToHTML = %q, want %q", got, want)
	}
}

func TestToHTMLReverseSwapsForegroundAndBackground(t *testing.T) {
	got := ToHTML("\x1b[1;33;44;7mX")
	want := `<span style="color:#0000AA;background-color:#FFFF55">X</span>`
	if got != want {
		t.Fatalf("ToHTML = %q, want %q", got, want)
	}
}

func TestToHTMLConvertsCRLFToBreak(t *testing.T) {
	got := ToHTML("a\r\nb")
	want := `<span style="color:#AAAAAA">a</span><br>` +
		`<span style="color:#AAAAAA">b</span>`
	if got != want {
		t.Fatalf("ToHTML = %q, want %q", got, want)
	}
}

func TestToHTMLEscapesHTMLSpecialCharacters(t *testing.T) {
	got := ToHTML("<b>&'\"")
	if !strings.Contains(got, "&lt;b&gt;&amp;") {
		t.Fatalf("ToHTML = %q, want escaped HTML entities", got)
	}
}

func TestToHTMLTranslatesCP437LineDrawingBytes(t *testing.T) {
	// 0xCD is a horizontal double-line in CP437 (U+2550).
	got := ToHTML(string([]byte{0xCD, 0xCD}))
	if !strings.Contains(got, "══") {
		t.Fatalf("ToHTML = %q, want CP437 0xCD translated to U+2550", got)
	}
}

func TestToHTMLDropsNonSGRCSISequences(t *testing.T) {
	// \x1b[2J (clear screen) and \x1b[H (cursor home) carry no meaning
	// in a static linear preview and must not appear or break output.
	got := ToHTML("\x1b[2J\x1b[HHello")
	want := `<span style="color:#AAAAAA">Hello</span>`
	if got != want {
		t.Fatalf("ToHTML = %q, want %q", got, want)
	}
}
