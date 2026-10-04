package bbs

import (
	"net"
	"strconv"
	"strings"
	"time"

	"git.maik.ch/nullmodem/kit/ansi"

	"git.maik.ch/nullmodem/bbs/internal/doors"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

// RLogin doors (kind "rlogin"): the door runs on another system -- a
// door network like DoorParty, or a friend's BBS -- reached over
// RLogin (RFC 1282). The BBS connects, sends the two user names the
// door server expects and the terminal type, and from then on passes
// bytes both ways until either side hangs up.

// rloginName fills in a remote door's user-name template.
func rloginName(tmpl string, u *user.User, node int) string {
	return strings.NewReplacer(
		"{handle}", u.Username,
		"{realname}", u.RealName,
		"{node}", strconv.Itoa(node),
		"{userid}", strconv.FormatInt(u.ID, 10),
	).Replace(tmpl)
}

// rloginHello is RLogin's opening: client user, server user and
// terminal/speed, each NUL-terminated, after a leading NUL.
func rloginHello(clientUser, serverUser, termType string) []byte {
	b := []byte{0}
	for _, f := range []string{clientUser, serverUser, termType} {
		b = append(b, f...)
		b = append(b, 0)
	}
	return b
}

// remoteEvent is what the caller typed, for playRemoteDoor.
type remoteEvent struct {
	data []byte
	err  error
}

func (s *Server) playRemoteDoor(term *Terminal, u *user.User, door doors.Door) error {
	r := door.Remote
	port := r.Port
	if port == 0 {
		port = 513
	}
	termType := r.TermType
	if termType == "" {
		termType = "ansi-bbs/115200"
	}
	addr := net.JoinHostPort(r.Host, strconv.Itoa(port))
	if err := term.Print(ansi.Reset + "\r\n" + ansi.FG(ansi.Yellow, true) +
		term.T("doors.connecting", "DOOR", door.Name) + ansi.Reset + "\r\n"); err != nil {
		return err
	}
	remote, err := net.DialTimeout("tcp", addr, 15*time.Second)
	if err != nil {
		s.logWarn("door %s: connecting to %s: %v", door.Name, addr, err)
		if err := term.Println(ansi.FG(ansi.Red, true) + term.T("doors.unreachable", "DOOR", door.Name) + ansi.Reset); err != nil {
			return err
		}
		return s.pauseForKey(term)
	}
	defer remote.Close()
	hello := rloginHello(rloginName(r.ClientUser, u, term.Node), rloginName(r.ServerUser, u, term.Node), termType)
	if _, err := remote.Write(hello); err != nil {
		return nil
	}
	// The server's NUL says it took the names.
	remote.SetReadDeadline(time.Now().Add(15 * time.Second))
	ack := make([]byte, 1)
	if _, err := remote.Read(ack); err != nil || ack[0] != 0 {
		s.logWarn("door %s: %s didn't accept the login", door.Name, addr)
		if err := term.Println(ansi.FG(ansi.Red, true) + term.T("doors.refused", "DOOR", door.Name) + ansi.Reset); err != nil {
			return err
		}
		return s.pauseForKey(term)
	}
	remote.SetReadDeadline(time.Time{})
	s.logInfo("%s played %s (%s)", u.Username, door.Name, addr)

	// Door server -> caller.
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		buf := make([]byte, 8<<10)
		for {
			n, err := remote.Read(buf)
			if n > 0 {
				if _, werr := term.Raw().Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	// Caller -> door server: one read at a time, each waiting for the
	// go-ahead, so nothing the caller types after the door ends is
	// swallowed by a read left hanging.
	events := make(chan remoteEvent)
	next := make(chan bool)
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := term.Raw().Read(buf)
			events <- remoteEvent{append([]byte(nil), buf[:n]...), err}
			if err != nil || !<-next {
				return
			}
		}
	}()
	for {
		select {
		case ev := <-events:
			if ev.err != nil {
				return ev.err
			}
			if _, err := remote.Write(ev.data); err != nil {
				next <- false
				return term.Println(ansi.Reset + "\r\n" + ansi.FG(ansi.Green, true) + term.T("doors.back_from", "DOOR", door.Name))
			}
			next <- true
		case <-closed:
			// The door hung up: the next key, already being waited for,
			// is the caller's way back.
			if err := term.Print(ansi.Reset + "\r\n" + ansi.FG(ansi.Green, true) + term.T("doors.back_from_key", "DOOR", door.Name) + ansi.Reset); err != nil {
				return err
			}
			ev := <-events
			next <- false
			if ev.err != nil {
				return ev.err
			}
			return term.Println("")
		}
	}
}
