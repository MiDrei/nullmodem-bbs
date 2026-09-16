package bbs

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

// pipeConn adapts a real net.Conn (from net.Pipe, which -- unlike
// fakeConn's static input buffer -- supports genuine concurrent
// bidirectional I/O) into the Conn interface, for the one test below
// that needs a real live byte stream: a real "rz" subprocess reading
// and responding to it on the other end, exercising this system's
// actual download path (Terminal.Raw() through to zmodem.Send) rather
// than the zmodem wire format in isolation (see internal/zmodem's own
// tests for that).
type pipeConn struct {
	net.Conn
}

func (c pipeConn) RemoteAddr() net.Addr   { return c.Conn.RemoteAddr() }
func (c pipeConn) TermType() string       { return "test" }
func (c pipeConn) WindowSize() (int, int) { return 80, 24 }

// TestDownloadFileSendsRealFileToRealRZOverTheBBSConnection is an
// end-to-end regression test through the actual BBS download command
// (not internal/zmodem directly): downloadFile must take over the
// connection's raw byte stream (see Terminal.Raw) and hand a real
// uploaded file to a real Zmodem receiver correctly, byte for byte,
// exactly as a caller's own terminal client would receive it.
func TestDownloadFileSendsRealFileToRealRZOverTheBBSConnection(t *testing.T) {
	if _, err := exec.LookPath("rz"); err != nil {
		t.Skip("rz (lrzsz) not installed; skipping real-interop download test")
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
	cmd := exec.Command("rz", "-y", "--disable-timeouts", "--quiet")
	cmd.Dir = recvDir
	cmd.Stdin = clientSide
	cmd.Stdout = clientSide
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting rz: %v", err)
	}

	downloadDone := make(chan error, 1)
	go func() {
		downloadDone <- s.downloadFile(term, u, uploaded)
	}()

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	select {
	case err := <-waitErr:
		if err != nil {
			t.Fatalf("rz exited with error: %v", err)
		}
	case <-time.After(15 * time.Second):
		cmd.Process.Kill()
		t.Fatal("rz did not exit in time")
	}
	clientSide.Close()

	select {
	case err := <-downloadDone:
		if err != nil {
			t.Fatalf("downloadFile: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("downloadFile did not return in time")
	}

	got, err := os.ReadFile(filepath.Join(recvDir, "readme.txt"))
	if err != nil {
		t.Fatalf("reading file rz received: %v", err)
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
