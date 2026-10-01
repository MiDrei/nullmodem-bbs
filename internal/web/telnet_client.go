package web

import (
	"bytes"
	"net"
	"sync"
)

// telnetClient is the web terminal's side of the Telnet connection to
// the BBS: it answers the BBS's option negotiation like a plain ANSI
// terminal would (binary, server echo, suppress go-ahead, terminal
// type "ANSI", window size), strips the protocol bytes from what goes
// to the browser and escapes 0xFF in what comes from it.
type telnetClient struct {
	conn net.Conn

	mu         sync.Mutex
	cols, rows int
	naws       bool // the BBS asked for the window size

	state int // where in an IAC sequence fromServer is
	cmd   byte
	sb    []byte
}

const (
	tIAC  = 255
	tDONT = 254
	tDO   = 253
	tWONT = 252
	tWILL = 251
	tSB   = 250
	tSE   = 240

	tBinary = 0
	tEcho   = 1
	tSGA    = 3
	tTType  = 24
	tNAWS   = 31
)

const (
	stData = iota
	stIAC
	stOpt
	stSB
	stSBIAC
)

func (t *telnetClient) send(b ...byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err := t.conn.Write(b)
	return err
}

func (t *telnetClient) sizeFrame() []byte {
	b := []byte{tIAC, tSB, tNAWS}
	for _, v := range []int{t.cols, t.rows} {
		hi, lo := byte(v>>8), byte(v)
		b = append(b, hi)
		if hi == tIAC {
			b = append(b, tIAC)
		}
		b = append(b, lo)
		if lo == tIAC {
			b = append(b, tIAC)
		}
	}
	return append(b, tIAC, tSE)
}

func (t *telnetClient) resize(cols, rows int) {
	t.mu.Lock()
	t.cols, t.rows = cols, rows
	naws := t.naws
	frame := t.sizeFrame()
	t.mu.Unlock()
	if naws {
		t.send(frame...)
	}
}

// fromServer handles bytes from the BBS: negotiation is answered, data
// returned for the browser.
func (t *telnetClient) fromServer(in []byte) ([]byte, error) {
	var out bytes.Buffer
	for _, b := range in {
		switch t.state {
		case stData:
			if b == tIAC {
				t.state = stIAC
			} else {
				out.WriteByte(b)
			}
		case stIAC:
			switch b {
			case tIAC:
				out.WriteByte(tIAC)
				t.state = stData
			case tDO, tDONT, tWILL, tWONT:
				t.cmd = b
				t.state = stOpt
			case tSB:
				t.sb = t.sb[:0]
				t.state = stSB
			default:
				t.state = stData
			}
		case stOpt:
			t.state = stData
			if err := t.negotiate(t.cmd, b); err != nil {
				return nil, err
			}
		case stSB:
			if b == tIAC {
				t.state = stSBIAC
			} else if len(t.sb) < 64 {
				t.sb = append(t.sb, b)
			}
		case stSBIAC:
			if b == tSE {
				t.state = stData
				// TTYPE SEND: we're an ANSI terminal.
				if len(t.sb) >= 2 && t.sb[0] == tTType && t.sb[1] == 1 {
					reply := append([]byte{tIAC, tSB, tTType, 0}, "ANSI"...)
					if err := t.send(append(reply, tIAC, tSE)...); err != nil {
						return nil, err
					}
				}
			} else {
				t.sb = append(t.sb, b)
				t.state = stSB
			}
		}
	}
	return out.Bytes(), nil
}

func (t *telnetClient) negotiate(cmd, opt byte) error {
	switch cmd {
	case tDO:
		switch opt {
		case tBinary, tSGA, tTType:
			return t.send(tIAC, tWILL, opt)
		case tNAWS:
			t.mu.Lock()
			t.naws = true
			frame := t.sizeFrame()
			t.mu.Unlock()
			return t.send(append([]byte{tIAC, tWILL, tNAWS}, frame...)...)
		}
		return t.send(tIAC, tWONT, opt)
	case tWILL:
		switch opt {
		case tBinary, tEcho, tSGA:
			return t.send(tIAC, tDO, opt)
		}
		return t.send(tIAC, tDONT, opt)
	}
	return nil // WONT/DONT: nothing to answer
}

// toServer sends the browser's keys, 0xFF doubled.
func (t *telnetClient) toServer(data []byte) error {
	if bytes.IndexByte(data, tIAC) >= 0 {
		data = bytes.ReplaceAll(data, []byte{tIAC}, []byte{tIAC, tIAC})
	}
	return t.send(data...)
}
