package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// The web terminal: a browser on /terminal opens a WebSocket here and
// gets a Telnet session on the BBS -- this daemon dials the bbs
// daemon's Telnet port, names the real caller in a PROXY line first
// (so lockouts and the per-address connection limit apply to them,
// not to us), speaks Telnet on that side and passes plain bytes to
// the browser, which draws them (lib/terminal).
//
// Browser to us: binary frames are keys (CP437 bytes); a text frame
// {"cols":80,"rows":25} is the window size. Us to browser: binary
// frames of what the BBS sends.

// TerminalAddr is set by cmd/web (config.WebConfig.TerminalAddr).

func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	if s.TerminalAddr == "" {
		writeError(w, http.StatusServiceUnavailable, "the web terminal is not set up")
		return
	}
	ip := clientIP(r)
	if s.Guard != nil {
		if v, err := s.Guard.Check(ip); err == nil && v.Blocked {
			writeError(w, http.StatusForbidden, v.Message())
			return
		}
	}
	ws, err := websocket.Accept(w, r, nil) // same origin only
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(64 << 10)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", s.TerminalAddr)
	if err != nil {
		s.logWarn("web terminal: connecting to %s: %v", s.TerminalAddr, err)
		ws.Close(websocket.StatusTryAgainLater, "the BBS is not reachable right now")
		return
	}
	defer conn.Close()
	family := "TCP4"
	if a := net.ParseIP(ip); a != nil && a.To4() == nil {
		family = "TCP6"
	}
	if _, err := fmt.Fprintf(conn, "PROXY %s %s %s 0 0\r\n", family, ip, ip); err != nil {
		return
	}

	t := &telnetClient{conn: conn, cols: 80, rows: 25}
	// BBS -> browser.
	go func() {
		defer cancel()
		buf := make([]byte, 16<<10)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				data, err2 := t.fromServer(buf[:n])
				if err2 != nil {
					return
				}
				if len(data) > 0 {
					wctx, wcancel := context.WithTimeout(ctx, 30*time.Second)
					err2 = ws.Write(wctx, websocket.MessageBinary, data)
					wcancel()
					if err2 != nil {
						return
					}
				}
			}
			if err != nil {
				ws.Close(websocket.StatusNormalClosure, "disconnected")
				return
			}
		}
	}()
	// Browser -> BBS.
	for {
		typ, data, err := ws.Read(ctx)
		if err != nil {
			return
		}
		if typ == websocket.MessageText {
			var size struct{ Cols, Rows int }
			if json.Unmarshal(data, &size) == nil && size.Cols >= 40 && size.Rows >= 10 && size.Cols <= 255 && size.Rows <= 255 {
				t.resize(size.Cols, size.Rows)
			}
			continue
		}
		if err := t.toServer(data); err != nil {
			return
		}
	}
}
