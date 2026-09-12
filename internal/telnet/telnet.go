// Package telnet implements a minimal RFC 854 telnet server with the
// option negotiation (IAC) needed for interactive BBS sessions: local
// echo suppression, suppress-go-ahead, terminal type, and NAWS window
// size reporting.
package telnet

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"sync"
)

// Telnet protocol bytes (RFC 854 / RFC 1091 / RFC 1073).
const (
	iac  = 255
	dont = 254
	do   = 253
	wont = 252
	will = 251
	sb   = 250
	se   = 240

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

// Write sends raw bytes to the client unmodified.
func (s *Session) Write(p []byte) (int, error) { return s.conn.Write(p) }

// Read returns decoded application data, transparently consuming and
// acting on any telnet IAC command sequences interleaved in the stream.
func (s *Session) Read(p []byte) (int, error) {
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
			continue
		}
		if err := s.handleCommand(); err != nil {
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

// handleCommand processes the telnet command following an IAC byte
// already consumed from the stream.
func (s *Session) handleCommand() error {
	cmd, err := s.r.ReadByte()
	if err != nil {
		return err
	}
	switch cmd {
	case iac:
		// Escaped 0xFF data byte; nothing further to do here since the
		// caller only invokes handleCommand for genuine IAC sequences.
		return nil
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
// asks the client to report window size and terminal type.
func (s *Session) negotiateInitial() error {
	seq := []byte{
		iac, will, optSGA,
		iac, will, optEcho,
		iac, do, optNAWS,
		iac, do, optTType,
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
