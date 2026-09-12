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
}
