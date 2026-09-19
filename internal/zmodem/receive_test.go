package zmodem

import (
	"bytes"
	"errors"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestReceiveRoundTripsThroughRealSexyz(t *testing.T) {
	requireSexyz(t)

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "readme.txt")
	content := []byte("hello zmodem world, uploaded via the real sexyz binary\n")
	if err := os.WriteFile(srcPath, content, 0o644); err != nil {
		t.Fatalf("writing source file: %v", err)
	}

	destDir, names, sendErr := sendAndReceiveInto(t, srcPath)
	if sendErr != nil {
		t.Fatalf("sexyz sender: %v", sendErr)
	}
	if len(names) != 1 || names[0] != "readme.txt" {
		t.Fatalf("received names = %v, want [readme.txt]", names)
	}
	got, err := os.ReadFile(filepath.Join(destDir, "readme.txt"))
	if err != nil {
		t.Fatalf("reading received file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("received content = %q, want %q", got, content)
	}
}

func TestReceiveMultiFileBatchRoundTripsThroughRealSexyz(t *testing.T) {
	requireSexyz(t)

	srcDir := t.TempDir()
	files := map[string][]byte{
		"one.txt":   []byte("first file\n"),
		"two.txt":   []byte("second file, a bit longer than the first one\n"),
		"three.bin": nil,
	}
	rng := rand.New(rand.NewSource(7))
	files["three.bin"] = make([]byte, 5000)
	rng.Read(files["three.bin"])

	var srcPaths []string
	for name, content := range files {
		p := filepath.Join(srcDir, name)
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		srcPaths = append(srcPaths, p)
	}
	sort.Strings(srcPaths)

	destDir, names, sendErr := sendAndReceiveInto(t, srcPaths...)
	if sendErr != nil {
		t.Fatalf("sexyz sender: %v", sendErr)
	}
	sort.Strings(names)
	var wantNames []string
	for name := range files {
		wantNames = append(wantNames, name)
	}
	sort.Strings(wantNames)
	if len(names) != len(wantNames) {
		t.Fatalf("received names = %v, want %v", names, wantNames)
	}
	for i := range names {
		if names[i] != wantNames[i] {
			t.Fatalf("received names = %v, want %v", names, wantNames)
		}
	}
	for name, content := range files {
		got, err := os.ReadFile(filepath.Join(destDir, name))
		if err != nil {
			t.Fatalf("reading received %s: %v", name, err)
		}
		if !bytes.Equal(got, content) {
			t.Fatalf("%s content = %d bytes, want %d bytes; mismatch", name, len(got), len(content))
		}
	}
}

// No TestReceiveEmptyFileRoundTripsThroughRealSexyz -- see send_test.go's
// identical note: real sexyz hangs indefinitely on a genuinely 0-byte
// file in either direction.

// TestReceiveReturnsErrFailedWhenSenderNeverStarts confirms Receive
// reports failure (rather than hanging or silently succeeding) when
// nothing on the other end ever speaks Zmodem to it.
func TestReceiveReturnsErrFailedWhenSenderNeverStarts(t *testing.T) {
	requireSexyz(t)

	destDir := t.TempDir()
	// An immediately-EOF reader, standing in for a connection that's
	// already gone -- mirrors Send's identically motivated test.
	conn := rwPair{Reader: bytes.NewReader(nil), Writer: io.Discard}

	done := make(chan error, 1)
	go func() {
		_, _, err := Receive(conn, destDir)
		done <- err
	}()

	select {
	case err := <-done:
		var failed *ErrFailed
		if !errors.As(err, &failed) {
			t.Fatalf("Receive = %v, want an *ErrFailed", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Receive did not return in time")
	}
}

// sendAndReceiveInto spawns a real sexyz sending every path in
// srcPaths, wired to Receive (the code under test) via pipes, and
// returns the fresh destination directory Receive wrote into along
// with the names it reported.
func sendAndReceiveInto(t *testing.T, srcPaths ...string) (destDir string, names []string, sendErr error) {
	t.Helper()

	destDir = t.TempDir()

	args := append([]string{"sz"}, srcPaths...)
	cmd := exec.Command("sexyz", args...)
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

	conn := rwPair{Reader: stdout, Writer: stdin}
	names, _, recvErr := Receive(conn, destDir)
	stdin.Close()

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case err := <-waitErr:
		if err != nil {
			t.Fatalf("sexyz sz exited with error: %v", err)
		}
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatal("sexyz sz did not exit in time")
	}

	return destDir, names, recvErr
}
