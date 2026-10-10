// Package proxied listens for connections a reverse proxy hands on --
// frp, HAProxy, Caddy's layer4 -- opening each with a PROXY protocol
// header (v1 or v2) that names the real caller. On such a port the
// header is required: a connection without one fails at its first
// read, so nobody can claim an address there, and the board's own
// ports stay as they are. A connection's RemoteAddr is the caller's.
package proxied

import (
	"net"
	"time"

	"github.com/pires/go-proxyproto"
)

// HeaderTimeout is how long a connection may take to send its header.
const HeaderTimeout = 10 * time.Second

// Listen listens on addr for proxied connections.
func Listen(addr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &proxyproto.Listener{Listener: ln, ReadHeaderTimeout: HeaderTimeout}, nil
}

// TCP is conn's TCP connection, also under a proxied one.
func TCP(conn net.Conn) (*net.TCPConn, bool) {
	if p, ok := conn.(*proxyproto.Conn); ok {
		return p.TCPConn()
	}
	tc, ok := conn.(*net.TCPConn)
	return tc, ok
}

// Serve runs serve on the listener for addr and, if proxyAddr is set,
// on a proxied one for it too, until either fails.
func Serve(addr, proxyAddr string, serve func(net.Listener) error) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	if proxyAddr == "" {
		return serve(ln)
	}
	pln, err := Listen(proxyAddr)
	if err != nil {
		return err
	}
	defer pln.Close()
	errc := make(chan error, 2)
	go func() { errc <- serve(ln) }()
	go func() { errc <- serve(pln) }()
	return <-errc
}
