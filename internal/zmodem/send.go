// Package zmodem sends and receives files to/from a BBS caller by
// shelling out to Synchronet's "sexyz" binary rather than
// reimplementing the Zmodem protocol from scratch.
//
// This package went through two earlier approaches before landing
// here, each abandoned for a documented, live-confirmed reason -- see
// git history for the full detail, since none of it is relevant to
// using the package today, only to why it looks like this:
//
//  1. A from-scratch native Go implementation (ZDLE escaping, ZBIN32/
//     ZHEX headers, ZRPOS/ZACK handling, adaptive streaming), verified
//     byte-for-byte against real lrzsz rz but still failing against a
//     real terminal client (SyncTERM) over a real network path with a
//     string of distinct bugs, the last of which had no further local
//     repro available to keep chasing. Every other BBS server checked
//     at that point (Synchronet, ENiGMA½) shells out instead of
//     reimplementing, for exactly this reason.
//  2. Shelling out to lrzsz (sz/rz), matching ENiGMA½'s own proven
//     config for it. This fixed the native implementation's failures,
//     but uncovered a run of new ones specific to lrzsz's own
//     assumption of a real serial line underneath it: fully-buffered
//     stdio when its stdout isn't a tty, and (even once run over a
//     real pseudo-terminal to fix that) SyncTERM's own Zmodem sender
//     periodically losing sync with rz's byte-position tracking in a
//     way that resisted every fix tried, on real hardware over a real
//     LAN, not just a slow/lossy link.
//  3. Synchronet's own "sexyz" (this package now), run with -telnet:
//     built from source (see docs/building-sexyz.md), Synchronet's
//     own reference Zmodem engine -- the one *SyncTERM itself* is
//     developed and benchmarked against (its author is a direct
//     contributor to sexyz's own error-recovery test harness) -- and,
//     unlike lrzsz, explicitly supports running over plain stdio
//     pipes with no serial-line assumptions baked in, confirmed by a
//     local round-trip test needing no pty at all. -telnet also moves
//     IAC escaping/unescaping into sexyz itself for both directions
//     of the wire, which is why this package needs telnet.Session's
//     SetRaw (see rawSwitcher) rather than doing any IAC handling of
//     its own the way the lrzsz-based version had to.
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

// rawSwitcher is implemented by a conn whose Read and Write can both
// be switched between interpreting/emitting telnet IAC sequences and
// passing every byte through completely untouched (telnet.Session's
// SetRaw -- see its own doc comment for the full story). Send and
// Receive both switch this on for sexyz's whole run when conn
// supports it, and pass sexyz -telnet in that case so sexyz takes
// over IAC handling on the raw stream entirely; a conn without this
// capability (an SSH channel, which has no telnet-layer framing to
// suspend or delegate in the first place) is simply left alone and
// sexyz is run without -telnet.
type rawSwitcher interface {
	SetRaw(raw bool)
}

// setRaw switches conn into raw mode for the duration of a transfer,
// if it supports doing so, and returns whether it did (so the caller
// knows whether to tell sexyz to expect a raw telnet stream) along
// with a func that turns raw mode back off again -- call the latter
// always, even when conn doesn't support this, via defer.
func setRaw(conn io.ReadWriter) (isTelnet bool, restore func()) {
	rs, ok := conn.(rawSwitcher)
	if !ok {
		return false, func() {}
	}
	rs.SetRaw(true)
	return true, func() { rs.SetRaw(false) }
}

// leftoverWait bounds how long Send/Receive will wait for their "conn
// -> sexyz stdin" copy to notice sexyz is done and stop, on a conn
// that can't be interrupted on demand (see Send). A var so this
// package's own tests can shrink it.
var leftoverWait = 3 * time.Second

// ErrFailed is returned by Send/Receive when sexyz exits reporting the
// transfer did not complete -- the far end cancelled it, declined the
// file, or the connection dropped partway through. Detail carries
// whatever diagnostic text sexyz did produce, for logging.
type ErrFailed struct {
	Detail string
}

func (e *ErrFailed) Error() string {
	if e.Detail == "" {
		return "zmodem: transfer did not complete"
	}
	return "zmodem: transfer did not complete: " + e.Detail
}

// Send transmits the file at path to conn using sexyz, run over plain
// pipes (no pseudo-terminal needed -- see this package's doc comment)
// and relayed to/from conn for the duration. conn should be the
// connection's raw byte stream (see Terminal.Raw) with any line-
// oriented/ANSI-cooking layer the rest of the BBS session normally
// applies bypassed -- Zmodem is an 8-bit binary protocol, not text.
//
// sexyz reports the file's name to the receiver as path's base name,
// so the caller is responsible for path actually being named the way
// it should appear on the receiving end (true of this project's own
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
	isTelnet, restore := setRaw(conn)
	defer restore()

	var args []string
	if isTelnet {
		args = append(args, "-telnet")
	}
	args = append(args, "-8", "sz", path)
	cmd := exec.Command("sexyz", args...)
	var stderr bytes.Buffer
	return runSexyz(cmd, conn, &stderr, "sexyz sz")
}

// runSexyz starts cmd (a fully-built sexyz invocation) with manually-
// managed stdin/stdout pipes, relays bytes between them and conn for
// the duration, and returns any leftover bytes the same way Send/
// Receive document.
//
// Deliberately not the simpler cmd.Stdin = conn / cmd.Stdout = conn:
// for any Stdin that isn't an *os.File, exec.Cmd copies it to the
// child's stdin pipe on a goroutine of its own, and Wait() blocks
// until that goroutine sees EOF or an error on conn, not just until
// the child process exits. conn is a live telnet/SSH session that
// stays open for the rest of the caller's BBS session -- it was never
// going to produce that EOF, so Wait() (and so this call, and so the
// BBS's whole download/upload command) would have hung forever after
// every single transfer, success or not. Managing the pipes directly
// keeps that wait scoped to what it should be: sexyz actually exiting.
func runSexyz(cmd *exec.Cmd, conn io.ReadWriter, stderr *bytes.Buffer, name string) ([]byte, error) {
	cmd.Stderr = stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("zmodem: creating %s stdin pipe: %w", name, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("zmodem: creating %s stdout pipe: %w", name, err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("zmodem: starting %s: %w", name, err)
	}

	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		io.Copy(conn, stdout)
	}()
	leftoverCh := make(chan []byte, 1)
	go func() { leftoverCh <- copyUntilClosed(stdin, conn) }()

	waitErr := cmd.Wait()

	// Both goroutines above must be done touching conn before this
	// returns: the caller resumes reading conn for ordinary keystrokes
	// right afterward, and a still-running reader here would race it
	// for whatever the caller types first (see copyUntilClosed for the
	// mechanics). stdout's side finishes on its own the moment sexyz's
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
	// immediately, since it's normally a reaction to the "transfer
	// complete" message the caller just printed) so the common case
	// still hands it back correctly, but give up and return rather
	// than hang the whole download/upload command -- and so the
	// caller's whole BBS session -- indefinitely if the caller simply
	// hasn't typed anything yet. copyUntilClosed keeps running in the
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
		return leftover, fmt.Errorf("zmodem: running %s: %w", name, waitErr)
	}
	return leftover, nil
}

// copyUntilClosed copies from src to dst until dst refuses a write
// (sexyz's stdin pipe, auto-closed once cmd.Wait sees the process
// exit) or src errors, returning whatever bytes it had already read
// from src but could no longer deliver to dst. That's the one case
// this can happen: src (conn) blocks waiting for the next byte right
// as sexyz exits, and the very next byte to arrive -- typically the
// caller's own first post-transfer keystroke, not anything meant for
// sexyz at all -- fails to write once dst has closed underneath it.
// Plain io.Copy would just drop that byte on the floor; the caller
// needs it back.
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
			// close dst (sexyz's stdin) to tell it the same rather
			// than leaving it to wait out its own timeout for a
			// handshake reply that was never going to arrive.
			dst.Close()
			return nil
		}
	}
}
