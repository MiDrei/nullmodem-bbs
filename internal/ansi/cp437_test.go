package ansi

import "testing"

func TestASCIIRangeRoundTrips(t *testing.T) {
	for b := byte(0x20); b < 0x7f; b++ {
		decoded := DecodeCP437([]byte{b})
		encoded := EncodeCP437(decoded)
		if len(encoded) != 1 || encoded[0] != b {
			t.Fatalf("byte 0x%02x: round trip got %v", b, encoded)
		}
	}
}

func TestDecodeCP437BoxDrawing(t *testing.T) {
	// 0xC9 is the CP437 double-line top-left box corner (U+2554).
	got := DecodeCP437([]byte{0xC9})
	want := "╔"
	if got != want {
		t.Fatalf("DecodeCP437(0xC9) = %q, want %q", got, want)
	}
}

func TestEncodeCP437UnmappableFallsBackToQuestionMark(t *testing.T) {
	got := EncodeCP437("中") // CJK character with no CP437 equivalent
	if len(got) != 1 || got[0] != '?' {
		t.Fatalf("EncodeCP437 unmappable rune = %v, want ['?']", got)
	}
}

// TestControlCharactersRoundTripAsThemselves is a regression test: a
// real LF/CR/ESC character in text being encoded to CP437 has no
// entry in runeToCP437 (only cp437ToRune's decorative "control
// picture" glyphs -- ☺, ♪, ⌂, ... -- do, at completely different
// Unicode code points), so EncodeCP437's fallback used to replace
// every one of them with '?' -- corrupting every line break and ANSI
// escape sequence in any text run through it. A genuine control byte
// must pass through as itself.
func TestControlCharactersRoundTripAsThemselves(t *testing.T) {
	for _, b := range []byte{0x00, 0x01, 0x07, 0x0a, 0x0d, 0x1b, 0x7f} {
		decoded := DecodeCP437([]byte{b})
		if len(decoded) != 1 || decoded[0] != b {
			t.Fatalf("DecodeCP437(0x%02x) = %q, want the literal control character", b, decoded)
		}
		encoded := EncodeCP437(decoded)
		if len(encoded) != 1 || encoded[0] != b {
			t.Fatalf("byte 0x%02x: round trip got %v", b, encoded)
		}
	}
}

// TestEncodeCP437PreservesRealANSIEscapeSequence confirms a real SGR
// escape sequence embedded in otherwise-Unicode text (the practical
// case this was fixed for: a message body that's genuine UTF-8 art
// mixed with real color codes) survives EncodeCP437 byte-for-byte.
func TestEncodeCP437PreservesRealANSIEscapeSequence(t *testing.T) {
	s := "\x1b[35m█▄\x1b[0m\nnext line"
	got := string(EncodeCP437(s))
	want := "\x1b[35m\xdb\xdc\x1b[0m\nnext line"
	if got != want {
		t.Fatalf("EncodeCP437(%q) = %q, want %q", s, got, want)
	}
}
