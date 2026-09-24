// Package bbs implements protocol-independent session handling: the
// node registry, the terminal I/O helpers, and the menu system. It
// knows nothing about telnet or SSH directly — either protocol handler
// just needs to satisfy the Conn interface.
package bbs

import (
	"io"
	"net"
)

// Conn is the minimal surface a transport (telnet, SSH) must expose so
// a session can be driven by the BBS menu system.
type Conn interface {
	io.ReadWriteCloser
	RemoteAddr() net.Addr
	TermType() string
	WindowSize() (width, height int)
	// Protocol reports which transport this connection came in on --
	// "telnet" or "ssh" -- purely so Server.Handle's own log lines can
	// tell the two apart (see the admin Logs page's Telnet/SSH tabs);
	// nothing in session/menu handling itself branches on it.
	Protocol() string
}
