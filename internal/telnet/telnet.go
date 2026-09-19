// Package telnet implements a minimal RFC 854 telnet server with the
// option negotiation (IAC) needed for interactive BBS sessions: local
// echo suppression, suppress-go-ahead, terminal type, and NAWS window
// size reporting.
package telnet

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// A note on RFC 856 TRANSMIT-BINARY, real clients simply not
// implementing it, and where this package's Zmodem support ended up:
// an earlier version tried negotiating BINARY (first for the whole
// session, then scoped to just a transfer) hoping Write/Read could
// skip IAC handling entirely for 8-bit data, gave up when a real
// client (SyncTERM) still needed IAC escaped regardless (confirmed by
// capturing it replying "IAC DONT <byte>" for a stray literal 0xFF in
// downloaded file data), dropped BINARY as apparently pointless, then
// re-added just the server->client half after finding it silently
// fixed a *different* bug (rz's CR-terminated headers, sent during an
// upload, arriving mangled -- NVT's rule that CR needs LF or NUL next,
// which BINARY relaxes). Accepting the OTHER direction too then
// uncovered a worse one: a raw byte capture proved SyncTERM's Zmodem
// *sender* never IAC-escapes its own outgoing bytes at all, regardless
// of what's negotiated, so ordinary Read was eating literal file-data
// 0xFF bytes as bogus commands, corrupting every upload past the first
// few hundred bytes.
//
// The eventual fix wasn't more negotiation -- it was accepting that
// this package's own IAC handling and a real client's Zmodem sender
// were never going to agree, and getting out of the way entirely for
// that stretch. This package shelled out to lrzsz (sz/rz) at first,
// managing IAC itself around it (Write escaping unconditionally,
// Read's SetRaw suspending interpretation only for the client's
// unescaped uploads) -- proven correct, but lrzsz's own assumption of
// a real serial line underneath caused a separate run of pty-handling
// bugs. Switching internal/zmodem to Synchronet's sexyz instead, run
// with -telnet, moved IAC handling into the external binary on BOTH
// sides of the wire: sexyz escapes/unescapes the raw stream itself,
// so this package's own handling would double-escape what it now
// already gets right. SetRaw therefore suspends Write's escaping too,
// not just Read's -- internal/zmodem (via the rawSwitcher interface)
// switches it on for sexyz's whole run and never needs BINARY
// negotiated for any of this to work.

// Telnet protocol bytes (RFC 854 / RFC 1091 / RFC 1073).
const (
	iac  = 255
	dont = 254
	do   = 253
	wont = 252
	will = 251
	sb   = 250
	se   = 240

	optBinary   = 0
	optEcho     = 1
	optSGA      = 3
	optTType    = 24
	optNAWS     = 31
	optLinemode = 34
)

// Session represents one connected telnet client. It implements
// io.ReadWriteCloser so it can be handled generically alongside SSH
// sessions by internal/bbs.
type Session struct {
	conn   net.Conn
	r      *bufio.Reader
	mu     sync.Mutex
	width  int
	height int
	term   string
	raw    bool
}

func newSession(conn net.Conn) *Session {
	return &Session{
		conn:   conn,
		r:      bufio.NewReader(conn),
		width:  80,
		height: 24,
		term:   "unknown",
	}
}

// RemoteAddr returns the client's network address.
func (s *Session) RemoteAddr() net.Addr { return s.conn.RemoteAddr() }

// TermType returns the negotiated terminal type, or "unknown" if the
// client never reported one.
func (s *Session) TermType() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.term
}

// WindowSize returns the last known terminal dimensions (defaults to
// 80x24 until/unless the client sends a NAWS update).
func (s *Session) WindowSize() (width, height int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.width, s.height
}

// Close closes the underlying connection.
func (s *Session) Close() error { return s.conn.Close() }

// SetReadDeadline delegates to the underlying net.Conn, letting a
// caller (internal/zmodem.Send, once its own subprocess has exited)
// interrupt a Read call already blocked waiting for the next byte,
// rather than having no way to reclaim that goroutine short of
// waiting for one to actually arrive.
func (s *Session) SetReadDeadline(t time.Time) error { return s.conn.SetReadDeadline(t) }

// SetRaw switches both Read and Write between their normal IAC-
// handling mode (raw=false, the default) and a raw passthrough that
// hands back/sends whatever bytes it's given completely unexamined,
// including any literal 0xFF -- see this file's top-of-file note for
// why: internal/zmodem's Send/Receive (via the rawSwitcher interface)
// call this for the duration of a Zmodem transfer run through sexyz
// with -telnet, which does its own IAC escaping/unescaping on both
// directions of the raw byte stream -- this package's own handling
// would otherwise double-escape (Write) or corrupt (Read, exactly as
// seen live: a real client's own Zmodem sender doesn't IAC-escape its
// outgoing bytes, which non-raw Read's command dispatch was silently
// eating as bogus commands) the exact same data sexyz is already
// escaping itself. Toggling mid-stream is safe -- any bytes already
// sitting in s.r's internal buffer from before the switch are still
// delivered via s.r itself either way, nothing is dropped or
// duplicated at the boundary.
func (s *Session) SetRaw(raw bool) {
	s.mu.Lock()
	s.raw = raw
	s.mu.Unlock()
}

// Write sends p to the client, doubling any literal IAC (0xFF) byte
// per RFC 854 -- unless SetRaw(true) is currently in effect, in which
// case p goes out completely unexamined (see SetRaw's doc comment for
// why a caller needs that).
func (s *Session) Write(p []byte) (int, error) {
	s.mu.Lock()
	raw := s.raw
	s.mu.Unlock()
	if raw || bytes.IndexByte(p, iac) < 0 {
		return s.conn.Write(p)
	}
	escaped := make([]byte, 0, len(p)+4)
	for _, b := range p {
		escaped = append(escaped, b)
		if b == iac {
			escaped = append(escaped, iac)
		}
	}
	if _, err := s.conn.Write(escaped); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Read returns decoded application data, transparently consuming and
// acting on any telnet IAC command sequences interleaved in the
// stream and unescaping a doubled IAC (IAC IAC) back into the single
// literal 0xFF data byte it represents -- unless SetRaw(true) is
// currently in effect, in which case none of that happens and Read
// simply hands back whatever bytes are on the wire (see SetRaw's
// doc comment for why a caller needs that).
//
// Read returns as soon as it has at least one application byte and
// nothing more is immediately available (s.r.Buffered() == 0),
// instead of blocking until p is completely full. This matters
// because a caller like bufio.Reader routinely asks for a large
// buffer's worth (4096 bytes); a real client's reply to one protocol
// frame is typically much shorter and is then followed by the client
// waiting for our own next frame rather than sending anything else --
// insisting on filling the whole buffer first blocked here for the
// full duration of that wait, confirmed live against a real Zmodem
// download: SyncTERM's prompt ZRINIT reply sat unread until our own
// unrelated 20-second header timeout elapsed and forced a retry,
// which SyncTERM (already past its own init, waiting for ZFILE)
// received as a bewildering stray ZRQINIT and reacted to with a burst
// of confused ZRINIT/ZABORT frames before giving up.
func (s *Session) Read(p []byte) (int, error) {
	s.mu.Lock()
	raw := s.raw
	s.mu.Unlock()
	if raw {
		return s.r.Read(p)
	}
	n := 0
	for n < len(p) {
		b, err := s.r.ReadByte()
		if err != nil {
			if n > 0 {
				return n, nil
			}
			return 0, err
		}
		if b != iac {
			p[n] = b
			n++
			if s.r.Buffered() == 0 {
				return n, nil
			}
			continue
		}
		cmd, err := s.r.ReadByte()
		if err != nil {
			return n, err
		}
		if cmd == iac {
			// Doubled IAC: one literal 0xFF data byte, not a command.
			// An earlier version of this dispatch discarded it here
			// silently instead of ever delivering it to the caller.
			p[n] = iac
			n++
			if s.r.Buffered() == 0 {
				return n, nil
			}
			continue
		}
		if err := s.handleCommand(cmd); err != nil {
			return n, err
		}
		if n > 0 {
			// Return what we have so far rather than blocking for more
			// application bytes after processing an out-of-band command.
			return n, nil
		}
	}
	return n, nil
}

// handleCommand processes the telnet command byte following an IAC
// already consumed from the stream (a doubled IAC, meaning literal
// data rather than a command, is handled by Read itself before this
// is ever called).
func (s *Session) handleCommand(cmd byte) error {
	switch cmd {
	case do, dont, will, wont:
		opt, err := s.r.ReadByte()
		if err != nil {
			return err
		}
		return s.negotiate(cmd, opt)
	case sb:
		return s.readSubnegotiation()
	default:
		// GA, NOP, and other commands with no arguments; ignore.
		return nil
	}
}

func (s *Session) negotiate(cmd, opt byte) error {
	switch opt {
	case optBinary:
		// Deliberately asymmetric -- see this file's top-of-file note
		// for the full story. Short version: accepting BINARY in the
		// server->client direction (client's "do" to our own "will")
		// fixed a real bug (rz's CR-terminated hex headers arriving
		// mangled), so that half stays accepted. But accepting it in
		// the OTHER direction too (replying "do" to the client's own
		// "will") caused a worse regression, confirmed live: a client
		// that also stops IAC-doubling ITS OWN outgoing bytes once
		// binary is active (RFC856 doesn't actually permit that, but a
		// real client did it anyway) fed literal, undoubled 0xFF file
		// bytes straight into Read's still-unconditional IAC handling,
		// which ate them as bogus command sequences -- silent data
		// corruption during a Zmodem upload, surfacing as repeated
		// ZRPOS retries and CRC errors. So the client's own "will" is
		// still declined ("dont"), keeping it escaping IAC exactly as
		// Read already requires.
		if cmd == will {
			return s.send(dont, opt)
		}
		if cmd == do {
			return s.send(will, opt)
		}
	case optNAWS:
		if cmd == will {
			return s.send(do, optNAWS)
		}
	case optTType:
		if cmd == will {
			if err := s.send(do, optTType); err != nil {
				return err
			}
			return s.requestTerminalType()
		}
	case optSGA, optEcho:
		if cmd == will {
			return s.send(do, opt)
		}
		if cmd == do {
			return s.send(will, opt)
		}
	default:
		if cmd == will || cmd == do {
			neg := byte(wont)
			if cmd == will {
				neg = dont
			}
			return s.send(neg, opt)
		}
	}
	return nil
}

func (s *Session) requestTerminalType() error {
	_, err := s.conn.Write([]byte{iac, sb, optTType, 1, iac, se})
	return err
}

func (s *Session) send(cmd, opt byte) error {
	_, err := s.conn.Write([]byte{iac, cmd, opt})
	return err
}

// readSubnegotiation reads an IAC SB ... IAC SE block and applies any
// NAWS or TTYPE payload it contains.
func (s *Session) readSubnegotiation() error {
	opt, err := s.r.ReadByte()
	if err != nil {
		return err
	}
	var payload []byte
	for {
		b, err := s.r.ReadByte()
		if err != nil {
			return err
		}
		if b == iac {
			next, err := s.r.ReadByte()
			if err != nil {
				return err
			}
			if next == se {
				break
			}
			// Escaped IAC within subnegotiation data.
			payload = append(payload, b)
			continue
		}
		payload = append(payload, b)
	}

	switch opt {
	case optNAWS:
		if len(payload) >= 4 {
			s.mu.Lock()
			s.width = int(payload[0])<<8 | int(payload[1])
			s.height = int(payload[2])<<8 | int(payload[3])
			s.mu.Unlock()
		}
	case optTType:
		if len(payload) >= 1 && payload[0] == 0 {
			s.mu.Lock()
			s.term = string(payload[1:])
			s.mu.Unlock()
		}
	}
	return nil
}

// negotiateInitial kicks off the option negotiation handshake expected
// by most telnet clients: server will suppress-go-ahead and echo, and
// asks the client to report window size and terminal type. Also
// offers WILL TRANSMIT-BINARY for our own server->client direction
// only (see this file's top-of-file note on why, and negotiate's
// optBinary case for why the other direction is deliberately never
// requested) -- a client that ignores the offer just keeps negotiating
// NVT ASCII as before, so this is safe to send unconditionally.
func (s *Session) negotiateInitial() error {
	seq := []byte{
		iac, will, optSGA,
		iac, will, optEcho,
		iac, do, optNAWS,
		iac, do, optTType,
		iac, will, optBinary,
	}
	_, err := s.conn.Write(seq)
	return err
}

// Server accepts telnet connections and hands each one to Handler in
// its own goroutine.
type Server struct {
	Addr    string
	Handler func(*Session)
}

// ListenAndServe binds Addr and serves connections until the listener
// errors (e.g. on Close from another goroutine).
func (srv *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("telnet: listen %s: %w", srv.Addr, err)
	}
	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			return fmt.Errorf("telnet: accept: %w", err)
		}
		if tc, ok := conn.(*net.TCPConn); ok {
			// Every reply this package (and internal/zmodem, riding on
			// top of it during a download) sends is a short, latency-
			// sensitive protocol message -- a telnet negotiation
			// response, a keystroke echo, a Zmodem header/ACK -- not a
			// bulk stream Nagle's algorithm's coalescing would help.
			// Left at Go's default, a real download over a real
			// higher-latency link (a VPN, confirmed live) needed many
			// such small round trips and gave a receiver with its own
			// tight per-block timeout (SyncTERM) more chances to give
			// up waiting than strictly necessary.
			_ = tc.SetNoDelay(true)
		}
		sess := newSession(conn)
		go func() {
			defer sess.Close()
			if err := sess.negotiateInitial(); err != nil {
				return
			}
			srv.Handler(sess)
		}()
	}
}

var _ io.ReadWriteCloser = (*Session)(nil)
