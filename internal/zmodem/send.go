// Package zmodem sends a file to a BBS caller by shelling out to the
// real "sz" binary (part of the lrzsz package) rather than
// reimplementing the Zmodem protocol from scratch.
//
// An earlier version of this package did reimplement it natively in
// Go, extensively verified against real lrzsz rz (byte-for-byte,
// including every 8-bit value, forced retries, and CRC checks by
// hand against captured wire bytes). It still kept failing against a
// real terminal client (SyncTERM) over a real, non-trivial network
// path (a WireGuard VPN in the testing that finally tracked this
// down) with a string of different real bugs found and fixed one at
// a time -- a blocking telnet Read that starved a receiver's short
// replies, missing IAC-escaping for Zmodem's own binary data, no
// reaction to a mid-transfer ZRPOS resend request, a receiver-
// specific block-size limit, needing per-block flow control instead
// of raw streaming, an unclosed frame confusing the receiver after a
// resend -- and after all of those, still hit a real CRC error
// streaming to that same client that a byte-identical stream to rz
// never showed, with no further local reproduction available to keep
// chasing it. Every other BBS server this project could find
// (Synchronet, ENiGMA½) does not reimplement Zmodem either -- they
// shell out to sz/rz (or Synchronet's own sexyz) for exactly this
// reason: those binaries are what real terminal clients' own Zmodem
// receivers are actually tested against, decades of real-world
// interop this package has no way to replicate from scratch.
package zmodem

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// deadliner is implemented by a conn that can have a pending Read
// call interrupted on demand (telnet.Session, wrapping a real
// net.Conn) -- see Send's use of it for why.
type deadliner interface {
	SetReadDeadline(t time.Time) error
}

// leftoverWait bounds how long Send will wait for its "conn -> sz
// stdin" copy to notice sz is done and stop, on a conn that can't be
// interrupted on demand (see Send). A var so this package's own tests
// can shrink it.
var leftoverWait = 3 * time.Second

// ErrFailed is returned by Send when sz exits reporting the transfer
// did not complete -- the receiver cancelled it, declined the file,
// or the connection dropped partway through. sz's own exit status
// doesn't reliably distinguish which (confirmed empirically: a
// receiver-cancelled transfer and a genuine I/O failure both exit 128
// with no differentiating detail on stderr), so this package doesn't
// try to either; Detail carries whatever diagnostic text sz did
// produce, for logging.
type ErrFailed struct {
	Detail string
}

func (e *ErrFailed) Error() string {
	if e.Detail == "" {
		return "zmodem: transfer did not complete"
	}
	return "zmodem: transfer did not complete: " + e.Detail
}

// Send transmits the file at path to conn using sz, piping sz's
// stdin/stdout directly through conn for the duration of the
// transfer. conn should be the connection's raw byte stream (see
// Terminal.Raw) with any line-oriented/ANSI-cooking layer the rest of
// the BBS session normally applies bypassed -- Zmodem is an 8-bit
// binary protocol, not text -- and, for a telnet connection
// specifically, must still be doing its own IAC-escaping in both
// directions (telnet.Session does): sz has no awareness of telnet
// framing at all, the same as any other external protocol handler
// (rz doesn't either; a BBS piping either through a raw telnet
// connection is responsible for that layer, not the zmodem binary).
//
// sz reports the file's name to the receiver as path's base name, so
// the caller is responsible for path actually being named the way it
// should appear on the receiving end (true of this project's own
// file storage: internal/file.Store lays files out under their
// original filename already).
//
// The returned leftover bytes, if any, must be fed back to whatever
// reads conn next (Terminal.Raw's caller pushes them into the
// Terminal's own pending buffer) rather than discarded -- see the
// unexported copyUntilClosed's doc comment for why they can exist at
// all: without handing them back, this cost the caller's own first
// keystroke or two right after every transfer, confirmed live.
func Send(conn io.ReadWriter, path string) ([]byte, error) {
	cmd := exec.Command("sz", "--binary", "--escape", "--quiet", path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// Deliberately not the simpler cmd.Stdin = conn / cmd.Stdout = conn
	// (which is how this package's own tests still wire up the *other*
	// side, a subprocess with nothing else to do once it exits): for
	// any Stdin that isn't an *os.File, exec.Cmd copies it to the
	// child's stdin pipe on a goroutine of its own, and Wait() blocks
	// until that goroutine sees EOF or an error on conn, not just
	// until the child process exits. conn is a live telnet/SSH
	// session that stays open for the rest of the caller's BBS
	// session -- it was never going to produce that EOF, so Wait()
	// (and so Send, and so the BBS's whole download command) would
	// have hung forever after every single transfer, success or not.
	// Managing the pipes directly keeps that wait scoped to what it
	// should be: the sz process actually exiting.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("zmodem: creating sz stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("zmodem: creating sz stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("zmodem: starting sz: %w", err)
	}

	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		io.Copy(conn, stdout)
	}()
	leftoverCh := make(chan []byte, 1)
	go func() { leftoverCh <- copyUntilClosed(stdin, conn) }()

	waitErr := cmd.Wait()

	// Both goroutines above must be done touching conn before Send
	// returns: the caller resumes reading conn for ordinary keystrokes
	// right afterward, and a still-running reader here would race it
	// for whatever the caller types first (see copyUntilClosed for the
	// mechanics; the fix that first surfaced this exact race is why
	// Send doesn't use the simpler cmd.Stdin/cmd.Stdout assignment
	// above at all). stdout's side finishes on its own the moment sz's
	// stdout pipe closes, which Wait() having already returned
	// guarantees already happened or is about to.
	<-stdoutDone

	// conn's copy only finishes once it next reads *something* -- if
	// conn supports interrupting a pending Read on demand (telnet.
	// Session does, wrapping a real net.Conn), force that now rather
	// than actually waiting for the caller's next keystroke to arrive
	// on its own, then clear the deadline again so it doesn't affect
	// this same conn's later, ordinary reads.
	if dl, ok := conn.(deadliner); ok {
		dl.SetReadDeadline(time.Now())
		defer dl.SetReadDeadline(time.Time{})
	}

	// Without that capability (e.g. an SSH channel, which has no
	// concept of a read deadline), fall back to bounding the wait
	// instead: block hoping the caller's next keystroke shows up
	// within leftoverWait (confirmed live: it usually arrives almost
	// immediately, since it's normally a reaction to the "Download
	// complete" message Send's caller just printed) so the common case
	// still hands it back correctly, but give up and return rather
	// than hang the whole download command -- and so the caller's
	// whole BBS session -- indefinitely if the caller simply hasn't
	// typed anything yet. copyUntilClosed keeps running in the
	// background past that point on this fallback path; a keystroke
	// that arrives after the bound is a rare, accepted residual risk
	// here, not eliminated the way it is above.
	var leftover []byte
	select {
	case leftover = <-leftoverCh:
	case <-time.After(leftoverWait):
	}

	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			return leftover, &ErrFailed{Detail: strings.TrimSpace(stderr.String())}
		}
		return leftover, fmt.Errorf("zmodem: running sz: %w", waitErr)
	}
	return leftover, nil
}

// copyUntilClosed copies from src to dst until dst refuses a write
// (sz's stdin pipe, auto-closed once cmd.Wait sees the process exit)
// or src errors, returning whatever bytes it had already read from
// src but could no longer deliver to dst. That's the one case this
// can happen: src (conn) blocks waiting for the next byte right as sz
// exits, and the very next byte to arrive -- typically the caller's
// own first post-transfer keystroke, not anything meant for sz at all
// -- fails to write once dst has closed underneath it. Plain io.Copy
// would just drop that byte on the floor; the caller needs it back.
func copyUntilClosed(dst io.WriteCloser, src io.Reader) []byte {
	buf := make([]byte, 4096)
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			written, werr := dst.Write(buf[:n])
			if werr != nil {
				return append([]byte(nil), buf[written:n]...)
			}
		}
		if rerr != nil {
			// src (conn) is done -- no more input is ever coming, so
			// close dst (sz's stdin) to tell it the same rather than
			// leaving it to wait out its own timeout for a handshake
			// reply that was never going to arrive.
			dst.Close()
			return nil
		}
	}
}
