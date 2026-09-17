// Package ssh implements the BBS's SSH terminal server on top of
// golang.org/x/crypto/ssh, exposing each accepted shell channel as a
// Session compatible with the same handler signature used by
// internal/telnet.
package ssh

import (
	"fmt"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
)

// Session represents one connected SSH client's interactive shell
// channel. It implements io.ReadWriteCloser.
type Session struct {
	channel ssh.Channel
	conn    *ssh.ServerConn

	mu     sync.Mutex
	width  int
	height int
	term   string
}

// RemoteAddr returns the client's network address.
func (s *Session) RemoteAddr() net.Addr { return s.conn.RemoteAddr() }

// TermType returns the terminal type reported in the pty-req, or
// "unknown" if none was sent.
func (s *Session) TermType() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.term == "" {
		return "unknown"
	}
	return s.term
}

// WindowSize returns the last known terminal dimensions.
func (s *Session) WindowSize() (width, height int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.width == 0 || s.height == 0 {
		return 80, 24
	}
	return s.width, s.height
}

// User returns the username presented during authentication.
func (s *Session) User() string { return s.conn.User() }

func (s *Session) Read(p []byte) (int, error)  { return s.channel.Read(p) }
func (s *Session) Write(p []byte) (int, error) { return s.channel.Write(p) }
func (s *Session) Close() error                { return s.channel.Close() }

// ptyRequestPayload mirrors the wire format of RFC 4254 4.5.1.
type ptyRequestPayload struct {
	Term                string
	Width, Height       uint32
	PixWidth, PixHeight uint32
	Modes               string
}

// Server accepts SSH connections and hands each interactive shell
// channel to Handler in its own goroutine. Since the BBS has no
// concept of SSH public-key identity yet, any username/password pair
// is accepted; real credential checks happen once the session reaches
// the BBS login menu, matching how the telnet listener behaves.
type Server struct {
	Addr    string
	HostKey ssh.Signer
	Handler func(*Session)
}

// ListenAndServe binds Addr and serves connections until the listener
// errors.
func (srv *Server) ListenAndServe() error {
	config := &ssh.ServerConfig{
		NoClientAuth: false,
		PasswordCallback: func(c ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			// Authentication is deferred to the BBS login menu; accept
			// any credentials here so the session can reach it.
			return &ssh.Permissions{}, nil
		},
	}
	config.AddHostKey(srv.HostKey)

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("ssh: listen %s: %w", srv.Addr, err)
	}
	defer ln.Close()

	for {
		nConn, err := ln.Accept()
		if err != nil {
			return fmt.Errorf("ssh: accept: %w", err)
		}
		if tc, ok := nConn.(*net.TCPConn); ok {
			// See internal/telnet's identical call for why: every reply
			// riding on this connection (including internal/zmodem's
			// download traffic) is a short, latency-sensitive message,
			// not a bulk stream Nagle's algorithm's coalescing would
			// help -- confirmed to matter live on a higher-latency
			// (VPN) link.
			_ = tc.SetNoDelay(true)
		}
		go srv.handleConn(nConn, config)
	}
}

func (srv *Server) handleConn(nConn net.Conn, config *ssh.ServerConfig) {
	sshConn, chans, reqs, err := ssh.NewServerConn(nConn, config)
	if err != nil {
		nConn.Close()
		return
	}
	defer sshConn.Close()

	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		sess := &Session{channel: channel, conn: sshConn}
		go srv.serveSession(sess, requests)
	}
}

// serveSession handles the request stream for one session channel:
// pty-req (captures term/width/height), shell/exec (starts the BBS
// handler), and window-change (live-updates dimensions). The BBS
// handler is run synchronously so the channel is closed as soon as it
// returns, ending the SSH session.
func (srv *Server) serveSession(sess *Session, requests <-chan *ssh.Request) {
	defer sess.Close()
	ready := make(chan struct{})
	started := false

	go func() {
		defer func() {
			if !started {
				close(ready)
			}
		}()
		for req := range requests {
			switch req.Type {
			case "pty-req":
				var payload ptyRequestPayload
				if ssh.Unmarshal(req.Payload, &payload) == nil {
					sess.mu.Lock()
					sess.term = payload.Term
					sess.width = int(payload.Width)
					sess.height = int(payload.Height)
					sess.mu.Unlock()
				}
				if req.WantReply {
					req.Reply(true, nil)
				}
			case "window-change":
				var payload struct{ Width, Height, PixWidth, PixHeight uint32 }
				if ssh.Unmarshal(req.Payload, &payload) == nil {
					sess.mu.Lock()
					sess.width = int(payload.Width)
					sess.height = int(payload.Height)
					sess.mu.Unlock()
				}
			case "shell", "exec":
				if req.WantReply {
					req.Reply(true, nil)
				}
				if !started {
					started = true
					close(ready)
				}
			default:
				if req.WantReply {
					req.Reply(false, nil)
				}
			}
		}
	}()

	<-ready
	srv.Handler(sess)
}
