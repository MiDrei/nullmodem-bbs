// Package zmodem implements the sending ("sz") half of the Zmodem
// file-transfer protocol, for a BBS caller downloading a file: what
// their own terminal client's Zmodem receiver ("rz" or its built-in
// equivalent -- SyncTERM, NetRunner, and most other modern BBS
// terminals all auto-detect and start one the moment they see this
// package's opening "rz\r" invite plus a ZRQINIT frame) pulls the
// bytes down through.
//
// Only the sender is implemented -- there's no upload support yet.
//
// The wire format below (frame layout, escaping rules, CRC choice and
// byte order, the ZFILE info-line shape) was derived from and
// verified against a real captured session between lrzsz's sz and rz
// (piped together locally, byte-for-byte hex-dumped and cross-checked
// against hand-computed CRC-16/CCITT and CRC-32 values), not just
// recalled from the spec -- see this package's tests, which replay
// the same interop check against real rz on every run.
package zmodem

import (
	"fmt"
	"hash/crc32"
)

// Protocol framing bytes.
const (
	zpad  = '*'  // pad character, doubled before a ZHEX header
	zdle  = 0x18 // Ctrl-X, the escape/frame-marker byte
	zdlee = zdle ^ 0x40

	frameZHEX   = 'B' // hex-encoded header (used for ZRQINIT/ZFIN)
	frameZBIN32 = 'C' // binary header with a trailing CRC-32
)

// Header frame types (the first byte of a header's 5-byte payload).
const (
	zrqinit = 0
	zrinit  = 1
	zfile   = 4
	zskip   = 5
	znak    = 6
	zabort  = 7
	zfin    = 8
	zrpos   = 9
	zdata   = 10
	zeof    = 11
	zcan    = 16
)

// Data subpacket end-of-block indicators, following a ZDLE byte.
const (
	zcrce = 'h' // frame ends; a header follows immediately
	zcrcg = 'i' // frame continues, streamed with no ACK expected
	zcrcw = 'k' // frame continues, ZACK expected; also releases XOFF
)

// ZRINIT flag bits, found in position byte 3 of a ZRINIT header --
// see decodeZRINITFlags.
const (
	rxCANFDX  = 0x01
	rxCANOVIO = 0x02
	rxCANFC32 = 0x20
)

// crc16ccitt computes the CRC-16/CCITT-FALSE-style checksum Zmodem's
// ZHEX headers use: polynomial 0x1021, initial value 0, no input/
// output reflection -- confirmed against a real captured ZRINIT
// header (crc16ccitt([0x01,0,0,0,0x23]) == 0xbe50).
func crc16ccitt(data []byte) uint16 {
	var crc uint16
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// needsEscape reports whether b must be sent as ZDLE b^0x40 rather
// than literally, wherever it appears inside a header or data
// subpacket: the frame-escape byte itself, and the flow-control bytes
// (XON/XOFF and their 8th-bit-set forms) real terminal/modem software
// might otherwise act on -- confirmed against a real captured ZEOF
// header, whose position byte (0x13, this file's 19-byte size) came
// through as ZDLE 'S' (0x13^0x40). Unlike some Zmodem implementations,
// plain CR is deliberately left unescaped here (matching real sz's
// own observed behavior for ordinary file data with no receiver-
// requested ESCCTL): full escaping of every control byte is a
// receiver-optional convenience for noisy serial links this system,
// running strictly over already-reliable TCP-backed telnet/SSH, has
// no use for.
func needsEscape(b byte) bool {
	switch b {
	case zdle, 0x10, 0x90, 0x11, 0x91, 0x13, 0x93:
		return true
	}
	return false
}

func putEscaped(buf []byte, b byte) []byte {
	if needsEscape(b) {
		return append(buf, zdle, b^0x40)
	}
	return append(buf, b)
}

// encodeHexHeader renders a ZHEX header frame (type + 4 position
// bytes + a CRC-16, all as lowercase ASCII hex pairs, terminated with
// CR, a linefeed with its 8th bit set, and an XON) -- used for
// ZRQINIT and ZFIN, matching real sz's own choice of hex framing
// specifically for those two (everything else uses encodeBin32Header
// instead).
func encodeHexHeader(typ byte, pos [4]byte) []byte {
	raw := [5]byte{typ, pos[0], pos[1], pos[2], pos[3]}
	crc := crc16ccitt(raw[:])
	buf := make([]byte, 0, 4+14+3)
	buf = append(buf, zpad, zpad, zdle, frameZHEX)
	buf = append(buf, []byte(fmt.Sprintf("%02x%02x%02x%02x%02x%02x%02x",
		raw[0], raw[1], raw[2], raw[3], raw[4], byte(crc>>8), byte(crc)))...)
	buf = append(buf, '\r', 0x8a, 0x11)
	return buf
}

// encodeBin32Header renders a ZBIN32 header frame: type + 4 position
// bytes + a little-endian CRC-32, each byte ZDLE-escaped where
// needed -- used for every header except ZRQINIT/ZFIN (see
// encodeHexHeader).
func encodeBin32Header(typ byte, pos [4]byte) []byte {
	raw := [5]byte{typ, pos[0], pos[1], pos[2], pos[3]}
	crc := crc32.ChecksumIEEE(raw[:])
	buf := make([]byte, 0, 3+5*2+4*2)
	buf = append(buf, zpad, zdle, frameZBIN32)
	for _, b := range raw {
		buf = putEscaped(buf, b)
	}
	for _, b := range crc32LE(crc) {
		buf = putEscaped(buf, b)
	}
	return buf
}

// encodeDataSubpacket renders one ZBIN32 data subpacket: data
// (escaped) followed by ZDLE+endType and the subpacket's own CRC-32
// -- computed over data plus the single endType byte, little-endian,
// escaped -- confirmed against two real captured subpackets (one
// ending zcrce, one zcrcw). A trailing XON follows only when endType
// is zcrcw (the flow-control release real sz sends there).
func encodeDataSubpacket(data []byte, endType byte) []byte {
	buf := make([]byte, 0, len(data)+8)
	for _, b := range data {
		buf = putEscaped(buf, b)
	}
	buf = append(buf, zdle, endType)

	h := crc32.NewIEEE()
	h.Write(data)
	h.Write([]byte{endType})
	for _, b := range crc32LE(h.Sum32()) {
		buf = putEscaped(buf, b)
	}
	if endType == zcrcw {
		buf = append(buf, 0x11)
	}
	return buf
}

func crc32LE(c uint32) [4]byte {
	return [4]byte{byte(c), byte(c >> 8), byte(c >> 16), byte(c >> 24)}
}

// leUint32 decodes 4 little-endian bytes -- the counterpart to
// crc32LE, used when decoding a received header's position field
// (e.g. ZRPOS's requested offset) or a subpacket's trailing CRC-32.
func leUint32(b [4]byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
