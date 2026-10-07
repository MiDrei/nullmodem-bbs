package bbs

import (
	"bytes"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/doors"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

// slowConn hands out its input a byte at a time, as slowly as someone
// typing, so the door server answers (and hangs up) in between.
type slowConn struct {
	*fakeConn
}

func (c *slowConn) Read(p []byte) (int, error) {
	time.Sleep(250 * time.Millisecond)
	return c.fakeConn.Read(p[:1])
}

func TestRLoginDoorLogsInAndPassesThrough(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	hello := make(chan []byte, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 256)
		n, _ := c.Read(buf)
		hello <- append([]byte(nil), buf[:n]...)
		c.Write([]byte{0})
		c.Write([]byte("Welcome to the door\r\n"))
		n, _ = c.Read(buf)
		c.Write([]byte("you typed " + string(buf[:n]) + "\r\n"))
		time.Sleep(100 * time.Millisecond)
	}()

	s := testServer(t)
	u, _ := s.Users.Register("alice", "password123", user.SLNewUser)
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	p, _ := strconv.Atoi(port)
	door := doors.Door{Name: "Door Party", Kind: "rlogin", Remote: config.RemoteDoor{
		Host: host, Port: p, ClientUser: "[NMB]{handle}", ServerUser: "secret",
	}}
	conn := &slowConn{fakeConn: newFakeConn("hXY")}
	term := NewTerminal(conn)
	term.Node = 3
	if err := s.playDoor(term, u, door); err != nil {
		t.Fatal(err)
	}
	if got := <-hello; !bytes.Equal(got, []byte("\x00[NMB]alice\x00secret\x00ansi-bbs/115200\x00")) {
		t.Fatalf("hello %q", got)
	}
	out := conn.out.String()
	if !strings.Contains(out, "Welcome to the door") || !strings.Contains(out, "you typed h") || !strings.Contains(out, "Back from Door Party") {
		t.Fatalf("screen: %q", out)
	}
	// X answered "press a key"; Y is still there for the BBS.
	k, err := term.ReadKey()
	if err != nil || k.Rune != 'Y' {
		t.Fatalf("next key %v %v, want Y", k, err)
	}
}
