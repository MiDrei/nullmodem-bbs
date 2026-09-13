package ansi

import "testing"

func TestVisibleWidthIgnoresANSIEscapes(t *testing.T) {
	s := "\x1b[1;36mHello\x1b[0m"
	if got := VisibleWidth(s); got != 5 {
		t.Fatalf("VisibleWidth() = %d, want 5", got)
	}
}

func TestLayoutSingleFillPadsToWidth(t *testing.T) {
	got := Layout("AreaName{FILL:.}42 msgs", 23)
	want := "AreaName........42 msgs" // "AreaName" (8) + "42 msgs" (7) = 15, so 23-15 = 8 dots
	if got != want {
		t.Fatalf("Layout() = %q, want %q", got, want)
	}
}

func TestVisibleWidthWithMixedANSIAndRawCP437Bytes(t *testing.T) {
	// A realistic box-border line: ESC color code, then raw CP437
	// bytes (0xC9 corner, 0xCD horizontal line, 0xBB corner) -- the
	// ESC sequence must still be stripped correctly even though the
	// rest of the string isn't valid UTF-8.
	s := "\x1b[1;36m\xc9\xcd\xcd\xcd\xbb"
	if got := VisibleWidth(s); got != 5 {
		t.Fatalf("VisibleWidth() = %d, want 5 (5 raw bytes, ESC sequence stripped)", got)
	}
}

func TestLayoutDoubleFillCentersRegardlessOfContentLength(t *testing.T) {
	short := Layout("\xba{FILL: }Hi{FILL: }\xba", 10)
	long := Layout("\xba{FILL: }Hello there{FILL: }\xba", 10)

	// Both must render at exactly the target width (2 border bytes +
	// filled interior), and the border bytes must stay at the very
	// start and end regardless of content length.
	if len(short) != 10 || short[0] != 0xba || short[len(short)-1] != 0xba {
		t.Fatalf("short = %q (len %d), want width 10 with border bytes preserved", short, len(short))
	}
	// "Hello there" (11 bytes) already exceeds the interior width (8),
	// so the fills should shrink to zero rather than go negative.
	if VisibleWidth(long) < 10 {
		t.Fatalf("long = %q, want at least width 10 when content overflows", long)
	}
}

func TestLayoutFillAcceptsRawCP437Byte(t *testing.T) {
	// 0xC9/0xCD/0xBB are real CP437 box-drawing bytes (corners and a
	// horizontal double-line), not ASCII -- the fill token must accept
	// them as the fill character, not just plain ASCII.
	got := Layout("\xc9{FILL:\xcd}\xbb", 6)
	want := "\xc9\xcd\xcd\xcd\xcd\xbb"
	if got != want {
		t.Fatalf("Layout() = %q, want %q", got, want)
	}
}

func TestLayoutMultipleLines(t *testing.T) {
	got := Layout("a{FILL:-}b\r\nc{FILL:-}d", 5)
	want := "a---b\r\nc---d"
	if got != want {
		t.Fatalf("Layout() = %q, want %q", got, want)
	}
}

func TestLayoutFillWithExplicitCountIgnoresWidth(t *testing.T) {
	// {FILL:x:N} repeats x exactly N times regardless of the target
	// width -- here 10 dashes even though width (40) would otherwise
	// call for far more.
	got := Layout("a{FILL:-:10}b", 40)
	want := "a" + repeatDash(10) + "b"
	if got != want {
		t.Fatalf("Layout() = %q, want %q", got, want)
	}
}

func repeatDash(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = '-'
	}
	return string(b)
}

func TestLayoutFillWithCountAndAutoFillOnSameLine(t *testing.T) {
	// The counted token takes exactly 5 dots regardless of width; the
	// bare token absorbs whatever's left of the 20-wide target after
	// "ab" (2) and the 5 dots are subtracted (20-2-5 = 13 dashes).
	got := Layout("a{FILL:.:5}b{FILL:-}", 20)
	want := "a....." + "b" + repeatDash(13)
	if got != want {
		t.Fatalf("Layout() = %q, want %q", got, want)
	}
}

func TestLayoutFillCountLargerThanWidthStillHonored(t *testing.T) {
	// An explicit count is authoritative even when it alone exceeds
	// the target width -- it's not clamped down to fit.
	got := Layout("{FILL:x:5}", 3)
	want := "xxxxx"
	if got != want {
		t.Fatalf("Layout() = %q, want %q", got, want)
	}
}

func TestLayoutFillMalformedCountFallsBackToLiteral(t *testing.T) {
	// No digits between the second colon and the closing brace, and a
	// count with no closing brace at all: both are malformed and must
	// be left untouched rather than crash or silently eat part of the
	// line.
	got := Layout("a{FILL:x:}b{FILL:-:12c", 20)
	want := "a{FILL:x:}b{FILL:-:12c"
	if got != want {
		t.Fatalf("Layout() = %q, want unchanged %q", got, want)
	}
}

func TestLayoutNoFillTokensReturnsUnchanged(t *testing.T) {
	got := Layout("plain line, no tokens", 80)
	if got != "plain line, no tokens" {
		t.Fatalf("Layout() = %q, want unchanged input", got)
	}
}

func TestCenterPadsShortStrings(t *testing.T) {
	got := Center("Hi", 10)
	want := "    Hi"
	if got != want {
		t.Fatalf("Center() = %q, want %q", got, want)
	}
}

func TestCenterLeavesOverlongStringsUnchanged(t *testing.T) {
	s := "this is definitely too long"
	if got := Center(s, 5); got != s {
		t.Fatalf("Center() = %q, want unchanged %q", got, s)
	}
}
