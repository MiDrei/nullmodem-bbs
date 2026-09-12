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
