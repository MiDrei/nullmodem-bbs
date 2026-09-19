// Package doors runs external "door" programs -- games and utilities
// a caller can launch from the BBS menu, handed the connection itself
// rather than driven through this project's own line-oriented/ANSI-
// cooked Terminal (see internal/bbs.Terminal.Raw, the same bypass
// internal/zmodem uses for file transfers).
//
// A door talks to its caller over whatever channel its BBS dropfile
// says to use. This package always writes a DOOR32.SYS dropfile (the
// modern, Linux/Windows-native successor to the classic DOS DOOR.SYS/
// DORINFOx.DEF formats -- see http://bbsfiles.com and any current door
// engine's own docs) naming comm type 2 (Telnet) and handle 3, and
// hands the door process an already-connected AF_UNIX socket at
// exactly that file descriptor via os/exec's ExtraFiles -- the door
// reads/writes that socket directly (recv()/send()), the same way it
// would an inherited descriptor under Mystic or ENiGMA½ on Linux (see
// ENiGMA½'s own docs on DOOR32.SYS socket descriptor sharing, which
// call this out as something Node.js *can't* do without an external
// bridge process; Go can, via ExtraFiles, with no bridge needed).
//
// This only works for a door whose own comm layer expects a raw
// socket in this way -- true of every actively maintained door with a
// native Linux/Windows port (that's what DOOR32.SYS was invented for).
// A classic DOS-only door needs a DOS emulator (DOSBox-X, DOSEMU2) and
// a FOSSIL driver bridging its own serial port to this same socket
// instead -- not implemented here yet; see docs/adding-a-door.md for
// the current, socket-native path this package actually supports.
package doors

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Door describes one external door program, as configured in
// configs/bbs.yaml.
type Door struct {
	// Name identifies this door in menus and logs.
	Name string
	// Exe is the path to the door's executable.
	Exe string
	// Dir is the working directory to run Exe from -- almost always
	// the door's own install directory, since doors commonly locate
	// their own data files (art, saved games) relative to cwd rather
	// than relative to the dropfile path.
	Dir string
	// Args are extra arguments passed before the dropfile path
	// argument this package appends itself (see Run).
	Args []string
	// MinSL is the minimum security level required to play.
	MinSL int
}

// Session carries the caller-specific fields Run writes into the
// DOOR32.SYS dropfile for one play session.
type Session struct {
	RealName        string
	Handle          string
	AccessLevel     int
	TimeLeftMinutes int
	Node            int
}

// isTelnetConn is implemented by a conn that can suspend its own
// telnet IAC interpretation/escaping for the duration of a raw byte
// stream -- see internal/telnet.Session.SetRaw and internal/zmodem's
// identically motivated rawSwitcher. Most doors built against
// DOOR32.SYS's socket mode (this package's Usurper included) do their
// own IAC escaping/negotiation on the wire, so this project's own
// telnet layer must get out of the way for the same reason sexyz's
// -telnet mode needs it.
type isTelnetConn interface {
	SetRaw(raw bool)
}

// telnetPreamble is the fixed 6-byte sequence a DOOR32.SYS-socket door
// commonly sends the instant it opens its comm channel: IAC WILL
// BINARY, IAC WILL ECHO (0xFF 0xFB 0x00, 0xFF 0xFB 0x01) -- real
// telnet negotiation, sent unconditionally, regardless of what the
// dropfile's own comm-type field said. A real telnet client's own
// telnet layer consumes this transparently, so it's harmless over
// telnet. An SSH channel has no telnet layer to consume it, so those
// six bytes would otherwise land on the caller's screen as visible
// garbage the instant a door starts -- see stripTelnetPreamble.
var telnetPreamble = []byte{0xFF, 0xFB, 0x00, 0xFF, 0xFB, 0x01}

// ErrFailed is returned by Run when the door process exits nonzero.
// Detail carries whatever it wrote to stderr, for logging.
type ErrFailed struct {
	Detail   string
	ExitCode int
}

func (e *ErrFailed) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("doors: %s exited with status %d", "door", e.ExitCode)
	}
	return fmt.Sprintf("doors: exited with status %d: %s", e.ExitCode, e.Detail)
}

// Run launches door, bridges conn to it for the duration of the play
// session, and returns once the door process exits. conn should be
// the connection's raw byte stream (see Terminal.Raw) -- like Zmodem,
// a door's own protocol (here, telnet-flavored socket I/O) has
// nothing to do with this Terminal's line-oriented/ANSI-cooked
// interaction.
func Run(conn io.ReadWriter, door Door, sess Session) error {
	isTelnet := false
	if rs, ok := conn.(isTelnetConn); ok {
		rs.SetRaw(true)
		isTelnet = true
		defer rs.SetRaw(false)
	}

	nodeDir, err := os.MkdirTemp("", "nullmodem-door-*")
	if err != nil {
		return fmt.Errorf("doors: creating scratch node dir: %w", err)
	}
	defer os.RemoveAll(nodeDir)

	if err := writeDoor32Sys(filepath.Join(nodeDir, "DOOR32.SYS"), sess); err != nil {
		return err
	}

	// SOCK_CLOEXEC keeps the door process from also inheriting *our*
	// end (parent) alongside the one it's actually meant to use --
	// without it, both ends survive its exec, so parent effectively
	// always has an open writer somewhere even after this function
	// closes its own copy, and the door's read on fd 3 then never
	// sees EOF on its own.
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("doors: creating socketpair: %w", err)
	}
	parent := os.NewFile(uintptr(fds[0]), door.Name+"-parent")
	child := os.NewFile(uintptr(fds[1]), door.Name+"-child")
	defer parent.Close()

	// A relative door.Exe must be resolved to an absolute path before
	// cmd.Dir is set to door.Dir -- confirmed live: exec.Cmd resolves
	// a relative Path against Dir (the *child's* working directory),
	// not this process's own, so "data/doors/foo/FOO.EXE" run with
	// Dir "data/doors/foo" fails looking for a doubled-up
	// ".../data/doors/foo/data/doors/foo/FOO.EXE" that doesn't exist.
	exe, err := filepath.Abs(door.Exe)
	if err != nil {
		return fmt.Errorf("doors: resolving %s's executable path: %w", door.Name, err)
	}

	args := append(append([]string{}, door.Args...), "/P"+nodeDir+"/")
	cmd := exec.Command(exe, args...)
	cmd.Dir = door.Dir
	cmd.ExtraFiles = []*os.File{child}
	cmd.Stdin = nil
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		child.Close()
		return fmt.Errorf("doors: starting %s: %w", door.Name, err)
	}
	// Our copy of the child's fd -- the door process has its own,
	// inherited at fork; closing ours doesn't affect it, and holding
	// it open would leave the socket with a reader/writer even after
	// the door exits, so parent would never see EOF below.
	child.Close()

	var doorOut io.Reader = parent
	if !isTelnet {
		doorOut = &preambleStripper{r: parent}
	}

	// A door session can end from either side: the door process exits
	// on its own (the caller quit the game from inside it -- the
	// normal case), or conn itself fails (the caller's connection
	// simply dropped mid-game). Whichever happens first drives the
	// other side's shutdown explicitly, rather than hoping each side
	// notices the other's half of a cooperative close on its own --
	// that cooperative approach (closing this end of the socketpair
	// and waiting for the door to notice EOF and exit by itself)
	// turned out to be genuinely racy in testing: a burst of
	// consecutive play sessions would intermittently leave the door
	// process never exiting at all, confirmed live via /proc/<pid>/fd
	// still showing its end of the socket open long after this
	// function should have returned. Racing an explicit signal in
	// each direction instead removes the guesswork entirely.
	cmdDone := make(chan error, 1)
	go func() { cmdDone <- cmd.Wait() }()

	doorToConnDone := make(chan struct{})
	go func() {
		defer close(doorToConnDone)
		io.Copy(conn, doorOut)
	}()
	connToDoorDone := make(chan struct{})
	go func() {
		defer close(connToDoorDone)
		io.Copy(parent, conn)
	}()

	var waitErr error
	select {
	case waitErr = <-cmdDone:
		// The door exited on its own -- the normal case.
	case <-doorToConnDone:
		// The door's own output stream ended without the process
		// having exited yet in a way cmdDone already reported (e.g.
		// it crashed and its socket end closed before we've observed
		// cmd.Wait() return) -- wait for the actual exit below.
		waitErr = <-cmdDone
	case <-connToDoorDone:
		// conn itself failed (the caller disconnected) -- the door
		// has no way to know that on its own, so make it exit rather
		// than leaving it running forever with nobody attached.
		cmd.Process.Kill()
		waitErr = <-cmdDone
	}

	// connToDoorDone is reading conn, which the door exiting first
	// (either select case above) does nothing to unblock on its own
	// -- the caller may not have typed anything since the door quit.
	// If conn supports interrupting a pending Read on demand, force
	// that now rather than actually waiting for the caller's next
	// keystroke to arrive on its own -- exactly internal/zmodem's
	// runSexyz does for the same reason, and the same bounded
	// fallback below when it can't. Harmless to do even when conn has
	// already failed on its own (the third select case above).
	if dl, ok := conn.(deadliner); ok {
		dl.SetReadDeadline(time.Now())
		defer dl.SetReadDeadline(time.Time{})
	}

	parent.Close()
	<-doorToConnDone

	// connToDoorDone's io.Copy is reading conn, not parent, so closing
	// parent above does nothing for it -- the deadline set above (or
	// conn's own failure, in the second/third select cases) is what
	// unblocks it. Bound the wait the same way runSexyz bounds its
	// otherwise-unkillable read when conn can't support a deadline.
	select {
	case <-connToDoorDone:
	case <-time.After(shutdownWait):
	}

	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			return &ErrFailed{Detail: strings.TrimSpace(stderr.String()), ExitCode: exitErr.ExitCode()}
		}
		return fmt.Errorf("doors: running %s: %w", door.Name, waitErr)
	}
	return nil
}

// deadliner is implemented by a conn that can have a pending Read
// call interrupted on demand -- see internal/zmodem's identically
// named, identically motivated interface.
type deadliner interface {
	SetReadDeadline(t time.Time) error
}

// shutdownWait bounds how long Run waits for connToDoorDone to notice
// a door-initiated shutdown on a conn that can't be interrupted on
// demand (see deadliner) -- a var so this package's own tests can
// shrink it.
var shutdownWait = 3 * time.Second

// preambleStripper drops the leading telnetPreamble bytes (if
// present) from the very start of an otherwise-untouched stream --
// see telnetPreamble's doc comment for why this only wraps a non-
// telnet (SSH) conn.
type preambleStripper struct {
	r      io.Reader
	offset int
}

func (p *preambleStripper) Read(buf []byte) (int, error) {
	for p.offset < len(telnetPreamble) {
		var b [1]byte
		n, err := p.r.Read(b[:])
		if n > 0 {
			// Only actually strip it if it matches byte-for-byte;
			// anything else (a door that doesn't send this preamble
			// at all) is passed straight through untouched instead of
			// silently eating its first few bytes of real output.
			if b[0] == telnetPreamble[p.offset] {
				p.offset++
				continue
			}
			mismatch := append(append([]byte(nil), telnetPreamble[:p.offset]...), b[0])
			p.offset = len(telnetPreamble)
			copy(buf, mismatch)
			return len(mismatch), nil
		}
		if err != nil {
			return 0, err
		}
	}
	return p.r.Read(buf)
}

// writeDoor32Sys writes the 11-line DOOR32.SYS dropfile format at
// path -- see https://raw.githubusercontent.com/NuSkooler/ansi-bbs/master/docs/dropfile_formats/door32_sys.txt
// for the full field-by-field spec. Comm type is always 2 (Telnet)
// and the comm/socket handle is always 3, matching the fixed fd Run
// hands the door process via ExtraFiles (Go always numbers a spawned
// child's ExtraFiles starting at 3, right after its inherited stdin/
// stdout/stderr).
func writeDoor32Sys(path string, sess Session) error {
	var b strings.Builder
	b.WriteString("2\n")                                     // Comm Type: 2=Telnet
	b.WriteString("3\n")                                     // Comm/Socket Handle
	b.WriteString("57600\n")                                 // Baud Rate (informational only over a socket)
	b.WriteString("NullModem BBS\n")                         // BBSID
	b.WriteString("1\n")                                     // User's Record Position
	b.WriteString(sess.RealName + "\n")                      // User's Real Name
	b.WriteString(sess.Handle + "\n")                        // User's Handle/Alias
	b.WriteString(strconv.Itoa(sess.AccessLevel) + "\n")     // User's Access Level
	b.WriteString(strconv.Itoa(sess.TimeLeftMinutes) + "\n") // User's Time Left (minutes)
	b.WriteString("1\n")                                     // Emulation: 1=Ansi
	b.WriteString(strconv.Itoa(sess.Node) + "\n")            // Current Node Number
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("doors: writing DOOR32.SYS: %w", err)
	}
	return nil
}
