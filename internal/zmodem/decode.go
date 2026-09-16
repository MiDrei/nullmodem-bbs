package zmodem

import (
	"bufio"
	"fmt"
	"hash/crc32"
	"strconv"
)

// readHeader reads the next zmodem header frame from r, skipping any
// leading non-protocol bytes (pad characters, a previous frame's own
// trailer) until it finds the ZDLE marker that starts one. Returns
// the frame's type and 4-byte position field. A frame kind this
// package doesn't recognize (the byte right after ZDLE) is treated as
// resync noise rather than an error -- the loop just keeps scanning
// for the next ZDLE.
func readHeader(r *bufio.Reader) (typ byte, pos [4]byte, err error) {
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, pos, err
		}
		if b != zdle {
			continue
		}
		kind, err := r.ReadByte()
		if err != nil {
			return 0, pos, err
		}
		switch kind {
		case frameZHEX:
			return readHexHeader(r)
		case frameZBIN32:
			return readBinHeader(r, 4)
		case 'A': // ZBIN, CRC-16 -- rare in practice (see this
			// package's doc comment: every real header observed from
			// rz used ZHEX), but spec-legal, so decoded for
			// completeness rather than treated as noise.
			return readBinHeader(r, 2)
		}
	}
}

// readHexHeader decodes a ZHEX header's 14 hex-digit payload (type +
// 4 position bytes + CRC-16, see encodeHexHeader) after its ZDLE 'B'
// marker has already been consumed by readHeader.
func readHexHeader(r *bufio.Reader) (typ byte, pos [4]byte, err error) {
	var raw [5]byte
	var crcBuf [2]byte
	for i := 0; i < 7; i++ {
		hi, err := r.ReadByte()
		if err != nil {
			return 0, pos, err
		}
		lo, err := r.ReadByte()
		if err != nil {
			return 0, pos, err
		}
		v, err := strconv.ParseUint(string([]byte{hi, lo}), 16, 8)
		if err != nil {
			return 0, pos, fmt.Errorf("zmodem: malformed hex header byte %q: %w", []byte{hi, lo}, err)
		}
		if i < 5 {
			raw[i] = byte(v)
		} else {
			crcBuf[i-5] = byte(v)
		}
	}
	got := uint16(crcBuf[0])<<8 | uint16(crcBuf[1])
	want := crc16ccitt(raw[:])
	if got != want {
		return 0, pos, fmt.Errorf("zmodem: hex header CRC mismatch (got %04x, want %04x)", got, want)
	}
	copy(pos[:], raw[1:5])
	return raw[0], pos, nil
}

// readBinHeader decodes a ZBIN/ZBIN32 header's type+position bytes
// and its trailing CRC (crcLen 2 for ZBIN, 4 for ZBIN32), unescaping
// ZDLE sequences throughout, after its ZDLE 'A'/'C' marker has
// already been consumed by readHeader.
func readBinHeader(r *bufio.Reader, crcLen int) (typ byte, pos [4]byte, err error) {
	raw, err := readEscaped(r, 5)
	if err != nil {
		return 0, pos, err
	}
	crcBytes, err := readEscaped(r, crcLen)
	if err != nil {
		return 0, pos, err
	}
	if crcLen == 4 {
		var b [4]byte
		copy(b[:], crcBytes)
		got := leUint32(b)
		want := crc32.ChecksumIEEE(raw)
		if got != want {
			return 0, pos, fmt.Errorf("zmodem: binary header CRC-32 mismatch (got %08x, want %08x)", got, want)
		}
	} else {
		got := uint16(crcBytes[0])<<8 | uint16(crcBytes[1])
		want := crc16ccitt(raw)
		if got != want {
			return 0, pos, fmt.Errorf("zmodem: binary header CRC-16 mismatch (got %04x, want %04x)", got, want)
		}
	}
	copy(pos[:], raw[1:5])
	return raw[0], pos, nil
}

// readEscaped reads n logical bytes from r, transparently unescaping
// any ZDLE b -> b^0x40 sequence along the way (see putEscaped).
func readEscaped(r *bufio.Reader, n int) ([]byte, error) {
	out := make([]byte, 0, n)
	for len(out) < n {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == zdle {
			next, err := r.ReadByte()
			if err != nil {
				return nil, err
			}
			b = next ^ 0x40
		}
		out = append(out, b)
	}
	return out, nil
}
