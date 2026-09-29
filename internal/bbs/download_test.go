package bbs

import (
	"bytes"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/user"
)

// pipeConn adapts a real net.Conn (from net.Pipe, which -- unlike
// fakeConn's static input buffer -- supports genuine concurrent
// bidirectional I/O) into the Conn interface, for the tests below
// that need a real live byte stream: a real "sexyz rz" subprocess
// reading and responding to it on the other end, exercising this
// system's actual download path (Terminal.Raw() through to
// zmodem.Send) rather than the zmodem wire format in isolation (see
// internal/zmodem's own tests for that). Does NOT implement
// zmodem.rawSwitcher (no SetRaw method) -- unlike a real
// telnet.Session, so Send/Receive run sexyz without -telnet against
// it here, exactly matching how they'd run over a raw SSH channel.
type pipeConn struct {
	net.Conn
}

func (c pipeConn) RemoteAddr() net.Addr   { return c.Conn.RemoteAddr() }
func (c pipeConn) TermType() string       { return "test" }
func (c pipeConn) Protocol() string       { return "test" }
func (c pipeConn) WindowSize() (int, int) { return 80, 24 }

// startRZ starts a real "sexyz rz" subprocess wired to conn (a
// net.Pipe end), receiving into dir. Deliberately does not use the
// simpler cmd.Stdin = conn / cmd.Stdout = conn -- the same reason
// internal/zmodem.Send's own doc comment gives for not doing that: conn
// is a live net.Pipe end nothing else closes here, so exec.Cmd's own
// internal stdin-copy goroutine (spun up for any Stdin that isn't an
// *os.File) would never see the EOF it waits for, and cmd.Wait()
// blocks on that goroutine finishing, not just on the process exiting
// -- confirmed live: this exact pattern is what made Send hang after
// every download before it was fixed to manage its pipes directly,
// and reusing the simpler assignment here hit the identical hang in
// this test itself.
//
// The returned channel is closed once everything rz wrote has been
// passed on to conn. Callers wait for it before cmd.Wait: Wait closes
// the stdout pipe as soon as rz has exited, and under load that
// dropped rz's last words -- its ZFIN answer to the sender -- leaving
// the BBS's sexyz sz waiting out its own timeouts (this file's long-
// flaky keystroke test).
func startRZ(t *testing.T, conn net.Conn, dir string) (*exec.Cmd, <-chan struct{}) {
	t.Helper()
	cmd := exec.Command("sexyz", "rz", dir+"/")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("sexyz StdinPipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("sexyz StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting sexyz rz: %v", err)
	}
	go func() {
		io.Copy(stdin, conn)
		stdin.Close()
		// rz has exited by the time the copy above ends (its stdin
		// pipe closing is what stopped it); nothing feeds conn to it
		// anymore, but downloadFile still has a trailing summary
		// message to write to the *server* side of this same net.Pipe
		// once Send returns, and net.Pipe's Write blocks until
		// something reads it. Keep draining so that write doesn't hang
		// the test forever waiting for a reader that quit existing.
		io.Copy(io.Discard, conn)
	}()
	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		io.Copy(conn, stdout)
	}()
	return cmd, stdoutDone
}

// TestDownloadFileSendsRealFileToRealRZOverTheBBSConnection is an
// end-to-end regression test through the actual BBS download command
// (not internal/zmodem directly): downloadFile must take over the
// connection's raw byte stream (see Terminal.Raw) and hand a real
// uploaded file to a real Zmodem receiver correctly, byte for byte,
// exactly as a caller's own terminal client would receive it.
func TestDownloadFileSendsRealFileToRealRZOverTheBBSConnection(t *testing.T) {
	if _, err := exec.LookPath("sexyz"); err != nil {
		t.Skip("sexyz not installed; skipping real-interop download test (see docs/building-sexyz.md)")
	}

	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.Files.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	content := []byte("hello from the BBS download command\n")
	uploaded, err := s.Files.UploadFile(area.ID, u.ID, "readme.txt", "", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	serverSide, clientSide := net.Pipe()
	defer serverSide.Close()
	defer clientSide.Close()
	term := NewTerminal(pipeConn{serverSide})

	recvDir := t.TempDir()
	cmd, rzOut := startRZ(t, clientSide, recvDir)

	downloadDone := make(chan error, 1)
	go func() {
		downloadDone <- s.downloadFile(term, u, uploaded)
	}()

	waitErr := make(chan error, 1)
	go func() { <-rzOut; waitErr <- cmd.Wait() }()

	select {
	case err := <-waitErr:
		if err != nil {
			t.Fatalf("sexyz rz exited with error: %v", err)
		}
	case <-time.After(15 * time.Second):
		cmd.Process.Kill()
		t.Fatal("sexyz rz did not exit in time")
	}

	select {
	case err := <-downloadDone:
		if err != nil {
			t.Fatalf("downloadFile: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("downloadFile did not return in time")
	}

	got, err := os.ReadFile(filepath.Join(recvDir, "readme.txt"))
	if err != nil {
		t.Fatalf("reading file sexyz rz received: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("received content = %q, want %q", got, content)
	}

	reloaded, err := s.Files.FileByID(uploaded.ID)
	if err != nil {
		t.Fatalf("FileByID: %v", err)
	}
	if reloaded.DownloadCount != 1 {
		t.Fatalf("DownloadCount = %d, want 1 (downloadFile should have recorded it)", reloaded.DownloadCount)
	}
}

// TestDownloadFileDoesNotLoseAKeystrokeRightAfterTheTransferEnds is a
// regression test for the actual bug found live: internal/zmodem's
// Send pipes the connection through a real "sexyz sz" subprocess, and
// the caller's own very next keystroke -- typed just after seeing
// "Download complete" -- was being silently read and discarded by a
// background copy goroutine that hadn't yet noticed sexyz was done,
// racing the Terminal's own subsequent read for it, costing the
// caller's first keystroke or two after every single download. This
// drives the real download path (through a real sexyz rz, same as the
// test above) and, once downloadFile has returned, writes one more byte on
// the client side simulating that keystroke -- the very next ReadKey
// must see it, not have it stolen by some other reader left running
// in the background.
func TestDownloadFileDoesNotLoseAKeystrokeRightAfterTheTransferEnds(t *testing.T) {
	if _, err := exec.LookPath("sexyz"); err != nil {
		t.Skip("sexyz not installed; skipping real-interop download test (see docs/building-sexyz.md)")
	}

	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.Files.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	content := []byte("hello from the BBS download command\n")
	uploaded, err := s.Files.UploadFile(area.ID, u.ID, "readme.txt", "", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	serverSide, clientSide := net.Pipe()
	defer serverSide.Close()
	defer clientSide.Close()
	term := NewTerminal(pipeConn{serverSide})

	recvDir := t.TempDir()
	cmd, rzOut := startRZ(t, clientSide, recvDir)

	downloadDone := make(chan error, 1)
	go func() {
		downloadDone <- s.downloadFile(term, u, uploaded)
	}()

	waitErr := make(chan error, 1)
	go func() { <-rzOut; waitErr <- cmd.Wait() }()

	select {
	case err := <-waitErr:
		if err != nil {
			t.Fatalf("sexyz rz exited with error: %v", err)
		}
	case <-time.After(15 * time.Second):
		cmd.Process.Kill()
		t.Fatal("sexyz rz did not exit in time")
	}

	select {
	case err := <-downloadDone:
		if err != nil {
			t.Fatalf("downloadFile: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("downloadFile did not return in time")
	}

	// downloadFile has returned; the "keystroke" ('q', a plain
	// KeyChar) simulates the caller reacting to "Download complete" --
	// written concurrently with ReadKey below since net.Pipe's Write
	// blocks until something reads it.
	go func() {
		if _, err := clientSide.Write([]byte("q")); err != nil {
			t.Errorf("writing simulated keystroke: %v", err)
		}
	}()

	keyDone := make(chan struct {
		key Key
		err error
	}, 1)
	go func() {
		key, err := term.ReadKey()
		keyDone <- struct {
			key Key
			err error
		}{key, err}
	}()

	select {
	case res := <-keyDone:
		if res.err != nil {
			t.Fatalf("ReadKey: %v", res.err)
		}
		if res.key.Type != KeyChar || res.key.Rune != 'q' {
			t.Fatalf("ReadKey = %+v, want KeyChar 'q' (the keystroke sent right after the transfer, not lost)", res.key)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadKey did not return in time -- the post-transfer keystroke appears to have been lost")
	}
}
