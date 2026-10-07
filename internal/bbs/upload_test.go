package bbs

import (
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/file"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

// startSZ starts a real "sexyz sz" subprocess wired to conn (a
// net.Pipe end), sending srcPath -- standing in for a caller's own
// terminal client uploading a file. See startRZ (download_test.go)
// for why the pipe management here is manual rather than the simpler
// cmd.Stdin = conn / cmd.Stdout = conn: the identical exec.Cmd hang
// this project hit on the download side applies here too.
func startSZ(t *testing.T, conn net.Conn, srcPath string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sexyz", "sz", srcPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("sexyz StdinPipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("sexyz StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting sexyz sz: %v", err)
	}
	go func() {
		io.Copy(stdin, conn)
		stdin.Close()
		// sexyz has exited by the time the copy above ends (its stdin
		// pipe closing is what stopped it); nothing feeds conn to it
		// anymore, but uploadFile still has a trailing summary message
		// to write to the *server* side of this same net.Pipe once
		// Receive returns, and net.Pipe's Write blocks until something
		// reads it. Keep draining so that write doesn't hang the test
		// forever waiting for a reader that quit existing.
		io.Copy(io.Discard, conn)
	}()
	go io.Copy(conn, stdout)
	return cmd
}

// TestUploadFileReceivesRealFileFromRealSexyzOverTheBBSConnection is
// an end-to-end regression test through the actual BBS upload command
// (not internal/zmodem directly): uploadFile must take over the
// connection's raw byte stream (see Terminal.Raw), receive a real
// file from a real Zmodem sender correctly byte for byte, and import
// it into the area's managed storage exactly the way a caller's own
// terminal client sending it would expect.
func TestUploadFileReceivesRealFileFromRealSexyzOverTheBBSConnection(t *testing.T) {
	if _, err := exec.LookPath("sexyz"); err != nil {
		t.Skip("sexyz not installed; skipping real-interop upload test (see docs/building-sexyz.md)")
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

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "notes.txt")
	content := []byte("hello BBS, this came in via the upload command\n")
	if err := os.WriteFile(srcPath, content, 0o644); err != nil {
		t.Fatalf("writing source file: %v", err)
	}

	serverSide, clientSide := net.Pipe()
	defer serverSide.Close()
	defer clientSide.Close()
	term := NewTerminal(pipeConn{serverSide})

	cmd := startSZ(t, clientSide, srcPath)

	uploadDone := make(chan error, 1)
	go func() {
		uploadDone <- s.uploadFile(term, u, area)
	}()

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	select {
	case err := <-waitErr:
		if err != nil {
			t.Fatalf("sexyz sz exited with error: %v", err)
		}
	case <-time.After(15 * time.Second):
		cmd.Process.Kill()
		t.Fatal("sexyz sz did not exit in time")
	}

	select {
	case err := <-uploadDone:
		if err != nil {
			t.Fatalf("uploadFile: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("uploadFile did not return in time")
	}

	files, err := s.Files.ListFiles(area.ID)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	var found *file.File
	for i := range files {
		if files[i].Filename == "notes.txt" {
			found = &files[i]
		}
	}
	if found == nil {
		t.Fatalf("notes.txt not found among area files: %+v", files)
	}
	got, err := os.ReadFile(found.StoragePath)
	if err != nil {
		t.Fatalf("reading imported file: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("imported content = %q, want %q", got, content)
	}
	if !found.UploadedBy.Valid || found.UploadedBy.Int64 != u.ID {
		t.Fatalf("UploadedBy = %+v, want valid and %d", found.UploadedBy, u.ID)
	}
}

// TestUploadFileRefusesWhenBelowAreasMinUploadSL confirms uploadFile
// checks the area's SL gate itself rather than relying solely on the
// menu system having already filtered the command out -- the same
// defense-in-depth downloadFile isn't tested for (an SL check only
// makes sense on the write path).
func TestUploadFileRefusesWhenBelowAreasMinUploadSL(t *testing.T) {
	s := testServer(t)
	// The very first user registered on a fresh install is always
	// auto-promoted to sysop (see user.Store.Register) -- consume that
	// with a throwaway account first so alice gets the SLNewUser level
	// actually requested below, needed for this test to mean anything.
	if _, err := s.Users.Register("root", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register root: %v", err)
	}
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.Files.CreateArea("restricted", "Restricted Area", "", "", 0, 100)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}

	conn := newFakeConn("")
	term := NewTerminal(conn)
	if err := s.uploadFile(term, u, area); err != nil {
		t.Fatalf("uploadFile: %v", err)
	}
	if files, err := s.Files.ListFiles(area.ID); err != nil {
		t.Fatalf("ListFiles: %v", err)
	} else if len(files) != 0 {
		t.Fatalf("ListFiles = %d entries, want 0 (upload should have been refused before Zmodem ever ran)", len(files))
	}
}
