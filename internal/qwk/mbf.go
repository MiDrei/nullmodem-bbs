package qwk

import (
	"encoding/binary"
	"math"
)

// float32ToMBF converts an IEEE-754 single-precision float to Microsoft
// Binary Format (MBF) single precision -- the 4-byte on-disk layout
// BASIC's MKS$ function used, and which QWK's .NDX index record
// pointers still use today (a QWK reader from the DOS era read them
// straight into BASIC's own numeric type). Algorithm verified against
// Julian Bucknall's documented MBF<->IEEE754 bit-twiddle
// (secondboyet.com/Articles/MBFSinglePrecision.html, itself derived
// from Microsoft's own published conversion routines) and cross-
// checked against the well-known reference encodings of 1.0 (on disk,
// little-endian: 00 00 00 81) and 2.0 (00 00 00 82) -- see mbf_test.go.
//
// v must be a small non-negative integer value (every caller in this
// package only ever converts a MESSAGES.DAT record number) -- true
// zero and true positive integers round-trip exactly; MBF's much
// smaller subnormal range near zero is deliberately not handled, since
// nothing here ever produces a value anywhere near it.
func float32ToMBF(v float32) [4]byte {
	bits := math.Float32bits(v)
	if bits&0x7F800000 == 0 {
		return [4]byte{}
	}
	mbf := (((bits & 0x7F800000) << 1) + 0x02000000) |
		((bits & 0x80000000) >> 8) |
		(bits & 0x007FFFFF)
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], mbf)
	return b
}

// mbfToFloat32 is float32ToMBF's inverse.
func mbfToFloat32(b [4]byte) float32 {
	mbf := binary.LittleEndian.Uint32(b[:])
	if mbf&0xFF000000 == 0 {
		return 0
	}
	bits := (((mbf - 0x02000000) & 0xFF000000) >> 1) |
		((mbf & 0x00800000) << 8) |
		(mbf & 0x007FFFFF)
	return math.Float32frombits(bits)
}
