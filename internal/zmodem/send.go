package zmodem

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"time"
)

// ErrCancelled is returned by Send when the receiver aborts the
// transfer (Zmodem's CAN sequence -- e.g. the caller hit their
// terminal's own cancel key).
var ErrCancelled = errors.New("zmodem: transfer cancelled by receiver")

// ErrSkipped is returned by Send when the receiver declines the file
// outright (Zmodem's ZSKIP -- e.g. it already has a file by that name
// and wasn't asked to overwrite).
var ErrSkipped = errors.New("zmodem: receiver skipped the file")

// blockSize is how many bytes of file content go into each data
// subpacket -- generous compared to Zmodem's historical 1024-byte
// default, which was sized for noisy serial lines; every telnet/SSH
// session here is already a fast, reliable TCP-backed link with
// nothing like that constraint.
const blockSize = 8192

// headerTimeout bounds how long Send waits for the receiver's next
// expected response before giving up or retrying (see
// sender.readHeaderTimeout).
const headerTimeout = 20 * time.Second

// handshakeRetries bounds how many times Send re-sends a handshake
// frame (ZRQINIT, ZFILE) while waiting for the matching response --
// generous enough to cover an auto-detecting terminal client needing
// a moment to start its own Zmodem receiver after seeing the "rz\r"
// invite.
const handshakeRetries = 5

// Send transmits r (size bytes, reported as filename with mtime) to
// conn using the Zmodem sender ("sz") protocol -- what a BBS caller's
// own terminal client auto-detects and receives when downloading a
// file (see this package's doc comment for how the wire format was
// derived and verified). conn should be the connection's raw byte
// stream for the duration of the call, with any line-oriented/ANSI-
// cooking layer the rest of the BBS session normally applies bypassed
// -- Zmodem is an 8-bit binary protocol, not text.
func Send(conn io.ReadWriter, r io.Reader, filename string, size int64, mtime time.Time) error {
	s := &sender{w: conn, r: bufio.NewReader(conn)}
	return s.run(r, filename, size, mtime)
}

type sender struct {
	w io.Writer
	r *bufio.Reader
}

func (s *sender) run(r io.Reader, filename string, size int64, mtime time.Time) error {
	if _, err := s.w.Write([]byte("rz\r")); err != nil {
		return fmt.Errorf("zmodem: sending invite: %w", err)
	}
	if err := s.handshake(); err != nil {
		return err
	}
	startAt, err := s.sendFileHeader(filename, size, mtime)
	if err != nil {
		return err
	}
	if err := s.sendData(r, startAt, size); err != nil {
		return err
	}
	return s.finish()
}

// handshake sends ZRQINIT and waits for the receiver's ZRINIT,
// re-sending up to handshakeRetries times in case the client's own
// Zmodem receiver hasn't started listening yet.
func (s *sender) handshake() error {
	for i := 0; i < handshakeRetries; i++ {
		if _, err := s.w.Write(encodeHexHeader(zrqinit, [4]byte{})); err != nil {
			return fmt.Errorf("zmodem: sending ZRQINIT: %w", err)
		}
		typ, _, err := s.readHeaderTimeout()
		if err != nil {
			continue
		}
		switch typ {
		case zrinit:
			return nil
		case zcan:
			return ErrCancelled
		}
		// An unexpected frame (a stray retransmit, say) -- loop
		// around and try again within budget.
	}
	return fmt.Errorf("zmodem: no ZRINIT received after %d attempts", handshakeRetries)
}

// sendFileHeader sends the ZFILE header naming filename/size/mtime
// and waits for the receiver's ZRPOS, re-sending on a timeout.
// Returns the byte offset ZRPOS asked to start from -- ordinarily 0,
// but honored as a genuine resume-from-offset request if not (see
// sendData).
func (s *sender) sendFileHeader(filename string, size int64, mtime time.Time) (startAt int64, err error) {
	info := fmt.Sprintf("%s\x00%d %o %o 0 %d %d\x00", filename, size, mtime.Unix(), 0o100644, 1, size)
	send := func() error {
		if _, err := s.w.Write(encodeBin32Header(zfile, [4]byte{})); err != nil {
			return fmt.Errorf("zmodem: sending ZFILE header: %w", err)
		}
		if _, err := s.w.Write(encodeDataSubpacket([]byte(info), zcrcw)); err != nil {
			return fmt.Errorf("zmodem: sending ZFILE info: %w", err)
		}
		return nil
	}
	if err := send(); err != nil {
		return 0, err
	}

	for i := 0; i < handshakeRetries; i++ {
		typ, pos, err := s.readHeaderTimeout()
		if err != nil {
			if i == handshakeRetries-1 {
				return 0, fmt.Errorf("zmodem: waiting for ZRPOS: %w", err)
			}
			if err := send(); err != nil {
				return 0, err
			}
			continue
		}
		switch typ {
		case zrpos:
			return int64(leUint32(pos)), nil
		case zskip:
			return 0, ErrSkipped
		case zcan:
			return 0, ErrCancelled
		}
		// A duplicate ZRINIT (observed live from real rz) or anything
		// else not the response we're waiting for -- keep waiting.
	}
	return 0, fmt.Errorf("zmodem: no ZRPOS received after %d attempts", handshakeRetries)
}

// sendData streams r's content (skipping the first startAt bytes,
// already on the receiver's side per its own ZRPOS) as consecutive
// ZDATA subpackets, unacknowledged (zcrcg) except the very last one
// of the file (zcrce, immediately followed by ZEOF) -- deliberately
// not waiting for a mid-file ACK the way a noisy serial link would
// need to: this system runs strictly over already-reliable TCP-backed
// telnet/SSH, which has nothing for that flow control to protect
// against.
func (s *sender) sendData(r io.Reader, startAt, size int64) error {
	if startAt > 0 {
		if seeker, ok := r.(io.Seeker); ok {
			if _, err := seeker.Seek(startAt, io.SeekStart); err != nil {
				return fmt.Errorf("zmodem: seeking to resume offset %d: %w", startAt, err)
			}
		} else if _, err := io.CopyN(io.Discard, r, startAt); err != nil {
			return fmt.Errorf("zmodem: discarding %d already-received bytes: %w", startAt, err)
		}
	}

	if _, err := s.w.Write(encodeBin32Header(zdata, posOf(startAt))); err != nil {
		return fmt.Errorf("zmodem: sending ZDATA header: %w", err)
	}

	buf := make([]byte, blockSize)
	sent := startAt
	if sent >= size {
		// Nothing to send (a zero-length file, or a resume that
		// starts exactly at EOF) -- real sz still sends one empty
		// subpacket ending the frame, confirmed against a real
		// captured zero-length-file session; a ZDATA header with no
		// subpacket at all before ZEOF left rz hanging indefinitely
		// in testing.
		if _, err := s.w.Write(encodeDataSubpacket(nil, zcrce)); err != nil {
			return fmt.Errorf("zmodem: sending empty terminating subpacket: %w", err)
		}
	}
	for sent < size {
		n, err := io.ReadFull(r, buf)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			return fmt.Errorf("zmodem: reading file content: %w", err)
		}
		if n == 0 {
			break
		}
		sent += int64(n)
		endType := byte(zcrcg)
		if sent >= size {
			endType = zcrce
		}
		if _, err := s.w.Write(encodeDataSubpacket(buf[:n], endType)); err != nil {
			return fmt.Errorf("zmodem: sending data: %w", err)
		}
	}

	if _, err := s.w.Write(encodeBin32Header(zeof, posOf(size))); err != nil {
		return fmt.Errorf("zmodem: sending ZEOF: %w", err)
	}
	return nil
}

// finish drains the receiver's post-ZEOF reply (a fresh ZRINIT,
// asking whether there's another file -- this package always sends
// exactly one) and its ZFIN echo, both best-effort: sending our own
// ZFIN and the closing "OO" proceeds regardless of what actually came
// back, or even a timeout, since both are required to end the session
// either way.
func (s *sender) finish() error {
	_, _, _ = s.readHeaderTimeout()

	if _, err := s.w.Write(encodeHexHeader(zfin, [4]byte{})); err != nil {
		return fmt.Errorf("zmodem: sending ZFIN: %w", err)
	}
	_, _, _ = s.readHeaderTimeout()

	if _, err := s.w.Write([]byte("OO")); err != nil {
		return fmt.Errorf("zmodem: sending OO: %w", err)
	}
	return nil
}

// readHeaderTimeout reads one header frame, bounded by headerTimeout.
// On timeout the underlying readHeader call is abandoned mid-read (it
// keeps running in its own goroutine until the connection eventually
// closes, at which point it exits on its own) -- an accepted trade-
// off for not threading read-deadline support through every possible
// connection type Send might be handed.
func (s *sender) readHeaderTimeout() (typ byte, pos [4]byte, err error) {
	type result struct {
		typ byte
		pos [4]byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		typ, pos, err := readHeader(s.r)
		ch <- result{typ, pos, err}
	}()
	select {
	case res := <-ch:
		return res.typ, res.pos, res.err
	case <-time.After(headerTimeout):
		return 0, pos, fmt.Errorf("zmodem: timed out waiting for a response")
	}
}

func posOf(n int64) [4]byte {
	return [4]byte{byte(n), byte(n >> 8), byte(n >> 16), byte(n >> 24)}
}
