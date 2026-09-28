// Package doors runs external "door" programs -- games and utilities
// a caller can launch from the BBS menu, handed the connection itself
// rather than driven through this project's own line-oriented/ANSI-
// cooked Terminal (see internal/bbs.Terminal.Raw, the same bypass
// bbskit/zmodem uses for file transfers).
//
// Two kinds of door are supported (Door.Kind), both ultimately handing
// the door process an already-connected AF_UNIX socket at fd 3 via
// os/exec's ExtraFiles -- the door reads/writes that socket directly,
// the same way it would an inherited descriptor under Mystic or
// ENiGMA½ on Linux (see ENiGMA½'s own docs on DOOR32.SYS socket
// descriptor sharing, which call this out as something Node.js *can't*
// do without an external bridge process; Go can, via ExtraFiles, with
// no bridge needed):
//
//   - "native" (the default): a door with its own native Linux/
//     Windows port that reads DOOR32.SYS's socket-handle field as a
//     raw inherited fd (that field is what DOOR32.SYS was invented
//     for) and talks to it directly -- no emulator involved. Usurper's
//     Linux port (see docs/adding-a-door.md) is this package's
//     reference example.
//   - "dosbox": a classic real-mode DOS door, run under DOSBox-X.
//     DOSBox-X's own nullmodem serial backend takes the *same*
//     inherited fd via its "-socket N" flag plus "inhsocket:1" in its
//     [serial] config, and bridges it to the guest's COM1 -- so the
//     DOS door itself is none the wiser that its "modem" is actually a
//     Unix socket this process created. This sidesteps DOSBox-X's own
//     TCP-based nullmodem listen/connect code, which was found to hang
//     indefinitely under Linux (a real, open upstream bug -- see
//     docs/adding-a-door.md) -- fd inheritance never goes through that
//     code path at all. DOSBox-X's own "telnet:1" serial option is
//     deliberately *not* used, even over a real telnet conn: with it
//     off, DOSBox-X's bridge is plain transparent passthrough with no
//     telnet awareness of its own, so Run leaves this project's own
//     telnet layer doing its ordinary IAC escaping/interpretation for
//     a "dosbox" door exactly as it would for normal Terminal output,
//     instead of switching to raw mode the way "native" doors need
//     (those handle IAC themselves -- see isTelnetConn below). Turning
//     on both at once would double-escape every 0xFF byte in the
//     door's own 8-bit CP437 output.
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
	"sync"
	"time"
)

// Door describes one external door program, as configured in
// configs/bbs.yaml.
type Door struct {
	// Name identifies this door in menus and logs.
	Name string
	// Kind selects how Run launches this door: "native" (default, the
	// zero value) for a door with its own native Linux/Windows port
	// (see Exe/Dir/Args below), or "dosbox" for a classic real-mode
	// DOS door run under DOSBox-X (see DOSBoxDir/DOSBoxLaunchCmd
	// below). See this package's doc comment for the full story.
	Kind string
	// MinSL is the minimum security level required to play.
	MinSL int

	// Exe is the path to the door's executable. Kind "native" only.
	Exe string
	// Dir is the working directory to run Exe from -- almost always
	// the door's own install directory, since doors commonly locate
	// their own data files (art, saved games) relative to cwd rather
	// than relative to the dropfile path. Kind "native" only.
	Dir string
	// Args are the door's command-line arguments. They may use the
	// placeholders "{dropfile}" (the absolute path of DOOR32.SYS),
	// "{dropfile_dir}" (its directory) and "{node}". Without any
	// placeholder, "/P<dropfile dir>/" is appended -- Usurper's switch,
	// and how doors were launched before placeholders existed. Kind
	// "native" only.
	Args []string

	// DOSBoxDir is the door's own install directory, mounted as C: in
	// the DOSBox-X guest. Kind "dosbox" only.
	DOSBoxDir string
	// DOSBoxLaunchCmd is the DOS command line that starts the door,
	// run from C: once mounted -- typically "CALL START.BAT" or a
	// direct EXE invocation, exactly as a sysop would type it at the
	// DOS prompt. The literal placeholder "{dropfile_dir}" is replaced
	// with the DOS path of the per-session drop file's own directory
	// (mounted as D:, i.e. "D:\") -- most classic doors take this via
	// a command-line switch (e.g. DOORWAY's "/s:") rather than a fixed
	// convention. Kind "dosbox" only.
	//
	// Further placeholders: "{dropfile}" is the DOS path of the drop
	// file itself (e.g. "D:\DOOR.SYS"), "{node}" the caller's node
	// number. Several lines run one after another, as in a batch file
	// -- e.g. a door that needs a converter run before the game itself.
	DOSBoxLaunchCmd string

	// DropFile selects the drop file format written for the session:
	// "door.sys", "dorinfo", "doorfile.sr" or "door32.sys" (see
	// DropFileFormats). Empty means the kind's own default: DOOR32.SYS
	// for "native" (its socket handle is how such a door finds its
	// connection, so it's always written), DOOR.SYS for "dosbox".
	DropFile string
	// DropFileInDoorDir also writes the drop file into the door's own
	// install directory (DOSBoxDir, or Dir for "native"), for the many
	// classic doors that look for it there rather than taking a path.
	// DORINFO is written as DORINFO<node>.DEF there as well, so
	// several nodes playing at once don't overwrite each other's.
	DropFileInDoorDir bool
	// LockFiles are files, relative to the door's install directory,
	// deleted before the door starts when nobody else is playing it --
	// stale node locks a crashed session left behind (e.g. OO2's
	// OONODE.DAT), which would otherwise lock everyone out.
	LockFiles []string
	// Stdio connects the door's stdin and stdout to the caller's
	// connection as well, for "native" doors that talk over standard
	// I/O the way Synchronet runs them (Usurper Reborn switches to that
	// by itself once its output is redirected) instead of over the
	// DOOR32.SYS socket handle. The BBS's own telnet layer then keeps
	// handling the protocol, as it does for "dosbox" doors, and a bare
	// LF in its output gets the CR a terminal driver would add.
	Stdio bool
	// ANSI16 rewrites the door's 256-colour and true-colour output into
	// the 16 classic ANSI colours, for doors drawn for modern terminals
	// (Immortal Barons) played from classic BBS terminals, which can't
	// show more -- see outputFilter.
	ANSI16 bool
}

// Session carries the caller-specific fields Run writes into the
// drop file for one play session.
type Session struct {
	RealName        string
	Handle          string
	AccessLevel     int
	TimeLeftMinutes int
	Node            int

	// UserID is the caller's user record number.
	UserID int64
	// TotalCalls and LastCall are the caller's own call statistics.
	TotalCalls int
	LastCall   time.Time
	// BBSName and SysopName are this board's own, for the drop file
	// formats that carry them.
	BBSName   string
	SysopName string
}

// isTelnetConn is implemented by a conn that can suspend its own
// telnet IAC interpretation/escaping for the duration of a raw byte
// stream -- see internal/telnet.Session.SetRaw and bbskit/zmodem's
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
	// Only a "native" door talks raw telnet-flavored bytes on the wire
	// itself (see isTelnetConn's doc comment) and so needs this
	// project's own telnet layer to step out of the way. A "dosbox"
	// door's DOSBox-X bridge deliberately runs without its own
	// "telnet:1" option (see dosboxConfigTemplate's doc comment), so
	// it's plain transparent passthrough with no telnet awareness of
	// its own -- this project's own telnet layer needs to keep doing
	// its normal IAC escaping/interpretation exactly as it would for
	// ordinary line-oriented Terminal output, or 8-bit CP437 door
	// output would reach a real telnet connection completely
	// unescaped.
	isTelnet := false
	if door.Kind != "dosbox" && !door.Stdio {
		if rs, ok := conn.(isTelnetConn); ok {
			rs.SetRaw(true)
			isTelnet = true
			defer rs.SetRaw(false)
		}
	}

	if err := validateDropFile(door); err != nil {
		return err
	}
	release, err := acquire(door)
	if err != nil {
		return err
	}
	defer release()

	nodeDir, err := os.MkdirTemp("", "nullmodem-door-*")
	if err != nil {
		return fmt.Errorf("doors: creating scratch node dir: %w", err)
	}
	defer os.RemoveAll(nodeDir)

	// Close-on-exec (see socketpair) keeps the door process from also
	// inheriting *our* end (parent) alongside the one it's actually
	// meant to use -- without it, both ends survive its exec, so parent
	// effectively always has an open writer somewhere even after this
	// function closes its own copy, and the door's read on fd 3 then
	// never sees EOF on its own.
	fds, err := socketpair()
	if err != nil {
		return fmt.Errorf("doors: creating socketpair: %w", err)
	}
	parent := os.NewFile(uintptr(fds[0]), door.Name+"-parent")
	child := os.NewFile(uintptr(fds[1]), door.Name+"-child")
	defer parent.Close()

	var cmd *exec.Cmd
	switch door.Kind {
	case "dosbox":
		cmd, err = buildDOSBoxCmd(nodeDir, door, sess)
	default:
		cmd, err = buildNativeCmd(nodeDir, door, sess)
	}
	if err != nil {
		child.Close()
		return err
	}
	cmd.ExtraFiles = []*os.File{child}
	cmd.Stdin = nil
	if door.Stdio {
		cmd.Stdin = child
		cmd.Stdout = child
	}
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
	doorOut = newOutputFilter(doorOut, door.Stdio, door.ANSI16)

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
	// keystroke to arrive on its own -- exactly bbskit/zmodem's
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

// buildNativeCmd prepares a Kind "native" door's process: a DOOR32.SYS
// dropfile and a direct invocation of the door's own executable (see
// this package's doc comment).
func buildNativeCmd(nodeDir string, door Door, sess Session) (*exec.Cmd, error) {
	if err := writeDoor32Sys(filepath.Join(nodeDir, "DOOR32.SYS"), sess); err != nil {
		return nil, err
	}
	if door.DropFile != "" && door.DropFile != DropFileDoor32Sys {
		if _, err := writeDropFile(nodeDir, door.DropFile, sess); err != nil {
			return nil, err
		}
	}
	if door.DropFileInDoorDir {
		format := door.DropFile
		if format == "" {
			format = DropFileDoor32Sys
		}
		if _, err := writeDropFile(door.Dir, format, sess); err != nil {
			return nil, err
		}
	}

	// A relative door.Exe must be resolved to an absolute path before
	// cmd.Dir is set to door.Dir -- confirmed live: exec.Cmd resolves
	// a relative Path against Dir (the *child's* working directory),
	// not this process's own, so "data/doors/foo/FOO.EXE" run with
	// Dir "data/doors/foo" fails looking for a doubled-up
	// ".../data/doors/foo/data/doors/foo/FOO.EXE" that doesn't exist.
	exe, err := filepath.Abs(door.Exe)
	if err != nil {
		return nil, fmt.Errorf("doors: resolving %s's executable path: %w", door.Name, err)
	}

	cmd := exec.Command(exe, nativeArgs(door.Args, nodeDir, sess.Node)...)
	cmd.Dir = door.Dir
	return cmd, nil
}

// dosboxConfigTemplate is a DOSBox-X config good enough to run any
// classic DOS door headlessly: enough CPU speed and XMS/EMS for a
// typical early-90s door, DOSBox-X's own built-in FOSSIL emulation
// (serial1_fossil=true -- no BNU.COM/X00.SYS TSR needed, and loading
// one anyway was confirmed live to sometimes hang a door's own COM
// port probing instead of helping), and MS-DOS 6.22 emulation
// (ver=6.22) specifically to keep long-filename support OFF -- a
// mounted Linux directory is otherwise case-sensitive in a way DOS
// itself never is, so two directories a sysop created as e.g. "Lord"
// and "LORD" from Linux would look like two different, wrong
// directories to the door once LFN is active.
//
// The trailing EXIT is not optional: a door exiting back to its own
// DOS prompt (e.g. typing "exit" out of a redirected shell door) does
// not end the DOSBox-X *process* on its own -- confirmed live, a
// caller who does this lands in an idle, fully interactive local DOS
// prompt still bridged to their connection, not back at this BBS's
// own menu, until they eventually hang up. EXIT right after the
// launch command ends DOSBox-X the moment the door itself returns,
// which is what actually drives Run's "door exited on its own" path.
const dosboxConfigTemplate = `[dosbox]
machine=svga_s3
memsize=4

[cpu]
core=auto
cputype=auto
cycles=10000

[serial]
serial1=nullmodem inhsocket:1 transparent:1
serial1_fossil=true

[dos]
xms=true
ems=true
ver=6.22

[autoexec]
MOUNT C %s
MOUNT D %s
C:
%s
EXIT
`

// buildDOSBoxCmd prepares a Kind "dosbox" door's process: a classic
// DOOR.SYS dropfile in its own per-session directory (mounted as D:),
// a generated per-session DOSBox-X config (door.DOSBoxDir mounted as
// C:, door.DOSBoxLaunchCmd run from it), and dosbox-x itself invoked
// with "-socket 3" so its [serial] "inhsocket:1" nullmodem backend
// picks up the fd Run hands it via ExtraFiles directly -- see this
// package's doc comment for why this sidesteps DOSBox-X's own
// TCP-based nullmodem entirely.
func buildDOSBoxCmd(nodeDir string, door Door, sess Session) (*exec.Cmd, error) {
	format := door.DropFile
	if format == "" {
		format = DropFileDoorSys
	}
	dropName, err := writeDropFile(nodeDir, format, sess)
	if err != nil {
		return nil, err
	}

	dosboxDir, err := filepath.Abs(door.DOSBoxDir)
	if err != nil {
		return nil, fmt.Errorf("doors: resolving %s's DOSBoxDir: %w", door.Name, err)
	}
	if door.DropFileInDoorDir {
		if _, err := writeDropFile(dosboxDir, format, sess); err != nil {
			return nil, err
		}
	}
	conf := fmt.Sprintf(dosboxConfigTemplate, dosboxDir, nodeDir, expandLaunchCmd(door.DOSBoxLaunchCmd, dropName, sess.Node))
	confPath := filepath.Join(nodeDir, "dosbox.conf")
	if err := os.WriteFile(confPath, []byte(conf), 0o644); err != nil {
		return nil, fmt.Errorf("doors: writing %s's DOSBox-X config: %w", door.Name, err)
	}

	cmd := exec.Command("dosbox-x", "-conf", confPath, "-socket", "3")
	// SDL never needs a real display for this -- confirmed live
	// against DOSBox-X's own -socket/inhsocket path, unlike its
	// TCP-based nullmodem modes (see this package's doc comment).
	cmd.Env = append(os.Environ(), "SDL_VIDEODRIVER=dummy")
	return cmd, nil
}

// deadliner is implemented by a conn that can have a pending Read
// call interrupted on demand -- see bbskit/zmodem's identically
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

// expandLaunchCmd fills in DOSBoxLaunchCmd's placeholders (see
// Door.DOSBoxLaunchCmd) and normalizes its lines for the autoexec
// section: one DOS command per line, blank lines dropped.
func expandLaunchCmd(cmd, dropName string, node int) string {
	r := strings.NewReplacer(
		"{dropfile_dir}", `D:\`,
		"{dropfile}", `D:\`+dropName,
		"{node}", strconv.Itoa(node),
	)
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(cmd, "\r\n", "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, r.Replace(line))
		}
	}
	return strings.Join(lines, "\n")
}

// active counts running sessions per door name, so Run only clears a
// door's LockFiles when nobody else is in it.
var (
	activeMu sync.Mutex
	active   = map[string]int{}
)

// acquire registers one more session of door, first deleting its
// LockFiles if it's the only one. The returned func unregisters it.
func acquire(door Door) (func(), error) {
	activeMu.Lock()
	defer activeMu.Unlock()
	if active[door.Name] == 0 && len(door.LockFiles) > 0 {
		base := door.DOSBoxDir
		if door.Kind != "dosbox" {
			base = door.Dir
		}
		for _, rel := range door.LockFiles {
			if !filepath.IsLocal(rel) {
				return nil, fmt.Errorf("doors: %s: lock file %q must be relative to the door's directory", door.Name, rel)
			}
			if err := removeCaseInsensitive(base, rel); err != nil {
				return nil, fmt.Errorf("doors: %s: removing lock file %s: %w", door.Name, rel, err)
			}
		}
	}
	active[door.Name]++
	return func() {
		activeMu.Lock()
		defer activeMu.Unlock()
		active[door.Name]--
	}, nil
}

// removeCaseInsensitive deletes base/rel, matching the last path
// element without regard to case -- DOS wrote the file, so it may be
// OONODE.DAT on disk when the config says oonode.dat. A missing file
// is not an error.
func removeCaseInsensitive(base, rel string) error {
	dir := filepath.Join(base, filepath.Dir(rel))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	want := filepath.Base(rel)
	for _, e := range entries {
		if strings.EqualFold(e.Name(), want) && !e.IsDir() {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// nativeArgs expands a native door's Args (see Door.Args).
func nativeArgs(args []string, nodeDir string, node int) []string {
	r := strings.NewReplacer(
		"{dropfile}", filepath.Join(nodeDir, "DOOR32.SYS"),
		"{dropfile_dir}", nodeDir+string(filepath.Separator),
		"{node}", strconv.Itoa(node),
	)
	out := make([]string, 0, len(args)+1)
	placeholders := false
	for _, a := range args {
		if e := r.Replace(a); e != a {
			placeholders = true
			a = e
		}
		out = append(out, a)
	}
	if !placeholders {
		out = append(out, "/P"+nodeDir+"/")
	}
	return out
}
