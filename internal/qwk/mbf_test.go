package qwk

import (
	"encoding/binary"
	"testing"
)

func TestFloat32ToMBFMatchesKnownReferenceEncodings(t *testing.T) {
	cases := []struct {
		v    float32
		want [4]byte // little-endian on-disk bytes, low byte first
	}{
		{0, [4]byte{0x00, 0x00, 0x00, 0x00}},
		{1, [4]byte{0x00, 0x00, 0x00, 0x81}},
		{2, [4]byte{0x00, 0x00, 0x00, 0x82}},
	}
	for _, c := range cases {
		got := float32ToMBF(c.v)
		if got != c.want {
			t.Errorf("float32ToMBF(%v) = % X, want % X", c.v, got, c.want)
		}
	}
}

func TestMBFRoundTripsSmallPositiveIntegers(t *testing.T) {
	for _, v := range []float32{0, 1, 2, 3, 100, 2500, 65535, 99999, 1234567} {
		b := float32ToMBF(v)
		got := mbfToFloat32(b)
		if got != v {
			t.Errorf("round trip %v: mbfToFloat32(float32ToMBF(%v)) = %v", v, v, got)
		}
	}
}

func TestMbfToFloat32MatchesKnownReferenceEncodings(t *testing.T) {
	cases := []struct {
		b    [4]byte
		want float32
	}{
		{[4]byte{0x00, 0x00, 0x00, 0x00}, 0},
		{[4]byte{0x00, 0x00, 0x00, 0x81}, 1},
		{[4]byte{0x00, 0x00, 0x00, 0x82}, 2},
	}
	for _, c := range cases {
		got := mbfToFloat32(c.b)
		if got != c.want {
			t.Errorf("mbfToFloat32(% X) = %v, want %v", c.b, got, c.want)
		}
	}
}

// TestMBFUint32SymmetryWithBinaryLittleEndian locks in that the on-disk
// byte order this package uses (b[0] = least significant) matches
// encoding/binary's LittleEndian convention exactly -- a silent byte-
// order mismatch here would still round-trip correctly within this
// package's own two functions while producing a file no real QWK
// reader could parse.
func TestMBFUint32SymmetryWithBinaryLittleEndian(t *testing.T) {
	b := float32ToMBF(1)
	got := binary.LittleEndian.Uint32(b[:])
	if got != 0x81000000 {
		t.Errorf("binary.LittleEndian.Uint32(float32ToMBF(1)) = %#x, want 0x81000000", got)
	}
}
