// Package binkp implements the BinkP protocol (FTS-1026, with the
// FTS-1027 CRAM-MD5 authentication extension) used by FidoNet-style
// mailer software (binkd and compatible hubs/nodes) to exchange mail
// packets and files over TCP. It implements the wire protocol only --
// the FTS-0001 packet format, tosser, and outbound scheduling that use
// it live in internal/mail.
package binkp

import (
	"encoding/binary"
	"fmt"
	"io"
)

// maxFrameLen is the largest payload a single frame can carry: BinkP's
// 2-byte frame header reserves its top bit for the command/data flag,
// leaving 15 bits (0-32767) for the length. A file larger than this
// is simply split across multiple data frames.
const maxFrameLen = 32767

// frameHeaderCommandBit marks a frame as a command frame (an M_*
// command plus its argument) rather than a data frame (raw file
// bytes) -- per FTS-1026, the bit SET means command, clear means
// data (easy to get backwards; this was in fact backwards here until
// a live test against a real binkp server -- which enforces the spec
// -- caught it. Round-tripping frames against this package's own
// reader, as the unit tests do, can't catch the bit being flipped
// consistently on both ends).
const frameHeaderCommandBit = 0x8000

// writeCommandFrame sends a command frame: 1 byte command ID followed
// by arg's bytes, wrapped in BinkP's 2-byte length header. arg is not
// null-terminated -- the frame length delimits it exactly, per spec.
func writeCommandFrame(w io.Writer, cmd Command, arg string) error {
	payload := make([]byte, 1+len(arg))
	payload[0] = byte(cmd)
	copy(payload[1:], arg)
	if len(payload) > maxFrameLen {
		return fmt.Errorf("binkp: command %s argument too long (%d bytes)", cmd, len(arg))
	}
	return writeFrame(w, false, payload)
}

// writeDataFrame sends one chunk of raw file data, up to maxFrameLen
// bytes -- callers with a larger buffer must call this repeatedly.
func writeDataFrame(w io.Writer, data []byte) error {
	if len(data) > maxFrameLen {
		return fmt.Errorf("binkp: data frame too large (%d bytes)", len(data))
	}
	return writeFrame(w, true, data)
}

func writeFrame(w io.Writer, isData bool, payload []byte) error {
	var header uint16
	if !isData {
		header = frameHeaderCommandBit
	}
	header |= uint16(len(payload))

	buf := make([]byte, 2+len(payload))
	binary.BigEndian.PutUint16(buf, header)
	copy(buf[2:], payload)

	if _, err := w.Write(buf); err != nil {
		return fmt.Errorf("binkp: write frame: %w", err)
	}
	return nil
}

// readFrame reads one frame and reports whether it's a data frame
// (isData) or a command frame, along with its raw payload -- for a
// command frame, payload[0] is the Command byte and payload[1:] is
// the argument string.
func readFrame(r io.Reader) (isData bool, payload []byte, err error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return false, nil, err
	}
	h := binary.BigEndian.Uint16(header[:])
	isData = h&frameHeaderCommandBit == 0
	length := h &^ frameHeaderCommandBit

	payload = make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return false, nil, fmt.Errorf("binkp: read frame payload: %w", err)
	}
	return isData, payload, nil
}
