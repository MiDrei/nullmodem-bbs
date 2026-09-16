package ansi

import (
	"strings"
	"testing"
)

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

// TestLayoutStripsStrayWhitespaceAfterLastFill locks in a real bug fix:
// a screen file shipped with a block of literal spaces left over after
// a line's trailing {FILL:x} token (editor debris from before the
// token was added). That dead weight counted against the line's
// measured width, leaving no room for {FILL:x} to actually pad it --
// so a banner meant to be centered ended up flush left, and a divider
// bar rendered as blank instead of a rule. Layout must drop that
// trailing padding automatically instead of requiring every screen
// author to notice and hand-fix it.
func TestLayoutStripsStrayWhitespaceAfterLastFill(t *testing.T) {
	// 40 stray trailing spaces after the closing {FILL: } would, left
	// in place, already consume the whole 20-column target width,
	// leaving nothing for the two fills to actually center "Hi" with.
	got := Layout("{FILL: }Hi{FILL: }"+strings.Repeat(" ", 40), 20)
	want := Layout("{FILL: }Hi{FILL: }", 20)
	if got != want {
		t.Fatalf("Layout() with stray trailing spaces = %q, want the same as without them: %q", got, want)
	}
	if VisibleWidth(got) != 20 {
		t.Fatalf("VisibleWidth() = %d, want exactly the target width 20", VisibleWidth(got))
	}
}

// TestLayoutStripsStrayANSICodesAfterLastFill checks the same fix
// tolerates trailing ANSI color codes mixed in with the stray spaces
// (as the real bug did -- a trailing "\x1b[1;30;40m" sat among the
// dead spaces), not just bare spaces.
func TestLayoutStripsStrayANSICodesAfterLastFill(t *testing.T) {
	got := Layout("{FILL: }Hi{FILL: }   \x1b[1;30;40m ", 20)
	want := Layout("{FILL: }Hi{FILL: }", 20)
	if got != want {
		t.Fatalf("Layout() with stray trailing spaces+ANSI = %q, want %q", got, want)
	}
}

// TestLayoutPreservesRealTrailingContentAfterLastFill checks the fix
// doesn't overreach: genuine trailing content after a {FILL:x} token
// (not just whitespace/color codes), like a dot-leader's trailing
// figure, must survive untouched.
func TestLayoutPreservesRealTrailingContentAfterLastFill(t *testing.T) {
	got := Layout("AreaName{FILL:.}42 msgs", 23)
	want := "AreaName........42 msgs"
	if got != want {
		t.Fatalf("Layout() = %q, want %q (real trailing content must be preserved)", got, want)
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

// TestStripLeadingScreenClearRemovesClearAndHome locks in a real
// production fix: a composable screen fragment (a row/columns/meta/
// footer template printed right after a full-screen header banner)
// that itself starts with a clear-screen+home sequence -- e.g. saved
// via the web ANSI designer, which defaults to one -- wipes out
// whatever was drawn before it and snaps the cursor back to row 1.
func TestStripLeadingScreenClearRemovesClearAndHome(t *testing.T) {
	s := "\x1b[2J\x1b[H\x1b[1;36mFrom: {FROM}"
	got := StripLeadingScreenClear(s)
	want := "\x1b[1;36mFrom: {FROM}"
	if got != want {
		t.Fatalf("StripLeadingScreenClear(%q) = %q, want %q", s, got, want)
	}
}

// TestStripLeadingScreenClearLeavesOrdinaryContentUnchanged confirms a
// fragment that never clears the screen (the common, correct case) is
// returned byte-for-byte unchanged.
func TestStripLeadingScreenClearLeavesOrdinaryContentUnchanged(t *testing.T) {
	s := "\x1b[1;36mFrom: {FROM}\r\nTo: {TO}"
	if got := StripLeadingScreenClear(s); got != s {
		t.Fatalf("StripLeadingScreenClear(%q) = %q, want unchanged", s, got)
	}
}

// TestStripLeadingScreenClearOnlyStripsLeading confirms a clear/home
// sequence appearing after real content -- unusual, and not the bug
// this guards against -- is left alone.
func TestStripLeadingScreenClearOnlyStripsLeading(t *testing.T) {
	s := "Row one\r\n\x1b[2J\x1b[HRow two"
	if got := StripLeadingScreenClear(s); got != s {
		t.Fatalf("StripLeadingScreenClear(%q) = %q, want unchanged (clear isn't leading)", s, got)
	}
}

func TestHasEscapeCodesTrueForRealANSI(t *testing.T) {
	s := "\x1b[1;33mHello\x1b[0m"
	if !HasEscapeCodes(s) {
		t.Fatalf("HasEscapeCodes(%q) = false, want true", s)
	}
}

func TestHasEscapeCodesFalseForPlainText(t *testing.T) {
	s := "just an ordinary message\nwith more than one line"
	if HasEscapeCodes(s) {
		t.Fatalf("HasEscapeCodes(%q) = true, want false", s)
	}
}

// TestHasArtBytesTrueForBlockDrawingCharacters locks in a real
// production fix: pure CP437 block/line-drawing art with no color
// codes at all (no ESC[ anywhere) still needs its exact spacing
// preserved -- HasEscapeCodes alone misses it, and WrapText's word-
// wrap collapses runs of spaces the artist placed deliberately.
func TestHasArtBytesTrueForBlockDrawingCharacters(t *testing.T) {
	s := "plain text then a block: \xdb\xdb\xdb and a line: \xc4\xc4\xc4"
	if !HasArtBytes(s) {
		t.Fatalf("HasArtBytes(%q) = false, want true", s)
	}
}

// TestHasArtBytesFalseForAccentedLatinProse confirms ordinary prose
// with accented Latin letters (CP437's lower high-byte range,
// 0x80-0xAF -- ü, ä, ö, ... -- plus ß at 0xE1) is never mistaken for
// art: German or French messages must still go through normal word-
// wrap.
func TestHasArtBytesFalseForAccentedLatinProse(t *testing.T) {
	// "Grüße aus München" using real CP437 code points: ü=0x81, ß=0xE1.
	s := "Gr\x81\xe1e aus M\x81nchen"
	if HasArtBytes(s) {
		t.Fatalf("HasArtBytes(%q) = true, want false (accented prose, not art)", s)
	}
}

func TestIsPreformattedTrueForEitherSignal(t *testing.T) {
	if !IsPreformatted("\x1b[1mcolored\x1b[0m") {
		t.Fatal("IsPreformatted() = false for ANSI escape codes, want true")
	}
	if !IsPreformatted("block art: \xdb\xdb\xdb") {
		t.Fatal("IsPreformatted() = false for block-drawing bytes, want true")
	}
	if IsPreformatted("just ordinary text") {
		t.Fatal("IsPreformatted() = true for plain prose, want false")
	}
}

// TestHasAlignedSpacingTrueForPlainASCIIFigletArt locks in a real
// production fix: a figlet-style logo/box built from ordinary ASCII
// punctuation (no CP437 high bytes, no escape codes at all) still
// depends on precise internal spacing -- confirmed live against a
// real fsxNet ad (a "GODS69 BBS" figlet logo) that WrapText turned
// into a diagonal staircase, each row's leading padding collapsed by
// a different amount, before this signal existed.
func TestHasAlignedSpacingTrueForPlainASCIIFigletArt(t *testing.T) {
	s := ": +   _____ _  __ ___ ____   ____  _____   + :\n" +
		":    /  ___| |/ // _ \\|  _ \\ |  _ \\ / ___| :\n" +
		": +                                    + :\n"
	if !HasAlignedSpacing(s) {
		t.Fatalf("HasAlignedSpacing(%q) = false, want true", s)
	}
	if !IsPreformatted(s) {
		t.Fatal("IsPreformatted() = false for plain-ASCII figlet art, want true")
	}
}

// TestHasAlignedSpacingFalseForOrdinaryProseWithOccasionalDoubleSpace
// checked this exact threshold against every message already tossed
// in on this system's live fsxNet/HobbyNet feeds: real replies
// sometimes have one accidental double-space (after a period, or a
// stray blank), but essentially never several lines' worth of a
// three-or-more-space internal run -- this must not treat those as
// art, or a normal reply would stop being word-wrapped.
func TestHasAlignedSpacingFalseForOrdinaryProseWithOccasionalDoubleSpace(t *testing.T) {
	s := "Got you here.  Welcome.\n\nMike Dippel\n\n \n\nThanks for the reply."
	if HasAlignedSpacing(s) {
		t.Fatalf("HasAlignedSpacing(%q) = true, want false (ordinary prose)", s)
	}
}

// TestHasAlignedSpacingRequiresMultipleLines guards the
// alignedSpacingMinLines threshold: a single hand-aligned line (e.g.
// one coincidentally spaced-out heading) shouldn't alone flip an
// otherwise ordinary message into verbatim/preformatted mode.
func TestHasAlignedSpacingRequiresMultipleLines(t *testing.T) {
	s := "Section:    Overview\n\nJust one ordinary paragraph after it."
	if HasAlignedSpacing(s) {
		t.Fatalf("HasAlignedSpacing(%q) = true, want false (only one aligned line)", s)
	}
}
