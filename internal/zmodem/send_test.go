package zmodem

import (
	"bytes"
	"errors"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// requireLRZSZ skips the test if the real lrzsz "sz"/"rz" binaries
// aren't installed -- these are interop checks against real Zmodem
// implementations, not just this package's own code, so there's no
// meaningful fallback without them.
func requireLRZSZ(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sz"); err != nil {
		t.Skip("sz (lrzsz) not installed; skipping real-interop Zmodem test")
	}
	if _, err := exec.LookPath("rz"); err != nil {
		t.Skip("rz (lrzsz) not installed; skipping real-interop Zmodem test")
	}
}

// rwPair adapts a separate reader and writer into the single
// io.ReadWriter Send expects, matching what a real telnet/SSH
// connection's raw byte stream looks like.
type rwPair struct {
	io.Reader
	io.Writer
}

func TestSendRoundTripsThroughRealRZ(t *testing.T) {
	requireLRZSZ(t)

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "readme.txt")
	content := []byte("hello zmodem world, sent via the real sz binary\n")
	if err := os.WriteFile(srcPath, content, 0o644); err != nil {
		t.Fatalf("writing source file: %v", err)
	}

	got := sendAndReceive(t, srcPath, "readme.txt")
	if !bytes.Equal(got, content) {
		t.Fatalf("received content = %q, want %q", got, content)
	}
}

func TestSendMultiBlockFileRoundTripsThroughRealRZ(t *testing.T) {
	requireLRZSZ(t)

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "bigfile.bin")
	rng := rand.New(rand.NewSource(42))
	content := make([]byte, 32*1024+777)
	if _, err := rng.Read(content); err != nil {
		t.Fatalf("generating random content: %v", err)
	}
	if err := os.WriteFile(srcPath, content, 0o644); err != nil {
		t.Fatalf("writing source file: %v", err)
	}

	got := sendAndReceive(t, srcPath, "bigfile.bin")
	if !bytes.Equal(got, content) {
		t.Fatalf("received %d bytes, want %d bytes; content mismatch", len(got), len(content))
	}
}

func TestSendEmptyFileRoundTripsThroughRealRZ(t *testing.T) {
	requireLRZSZ(t)

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "empty.txt")
	if err := os.WriteFile(srcPath, nil, 0o644); err != nil {
		t.Fatalf("writing source file: %v", err)
	}

	got := sendAndReceive(t, srcPath, "empty.txt")
	if len(got) != 0 {
		t.Fatalf("received content = %q, want empty", got)
	}
}

// TestSendReturnsErrFailedWhenReceiverNeverStarts confirms Send
// reports failure (rather than hanging or silently succeeding) when
// nothing on the other end ever speaks Zmodem back.
func TestSendReturnsErrFailedWhenReceiverNeverStarts(t *testing.T) {
	if _, err := exec.LookPath("sz"); err != nil {
		t.Skip("sz (lrzsz) not installed; skipping")
	}

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "readme.txt")
	if err := os.WriteFile(srcPath, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("writing source file: %v", err)
	}

	// An immediately-EOF reader, standing in for a connection that's
	// already gone -- sz notices there's nothing to read from and
	// gives up on its own quickly (confirmed manually: piping from
	// /dev/null exits 128 almost immediately), unlike a genuinely
	// silent-but-open connection, which sz will wait out for as long
	// as its own (here, default) timeout allows.
	conn := rwPair{Reader: bytes.NewReader(nil), Writer: io.Discard}

	done := make(chan error, 1)
	go func() {
		_, err := Send(conn, srcPath)
		done <- err
	}()

	select {
	case err := <-done:
		var failed *ErrFailed
		if !errors.As(err, &failed) {
			t.Fatalf("Send = %v, want an *ErrFailed", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Send did not return in time")
	}
}

// sendAndReceive runs Send (against the real sz binary) piping into a
// real "rz" subprocess (writing into a fresh temp directory), and
// returns the bytes rz actually wrote to disk for wantFilename, or
// fails the test. srcPath is the file Send is told to transmit;
// wantFilename is the name it should arrive under (sz reports
// srcPath's own base name, so this also implicitly checks that).
func sendAndReceive(t *testing.T, srcPath, wantFilename string) []byte {
	t.Helper()

	dir := t.TempDir()
	cmd := exec.Command("rz", "-y", "--disable-timeouts", "--quiet")
	cmd.Dir = dir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting rz: %v", err)
	}

	conn := rwPair{Reader: stdout, Writer: stdin}
	_, sendErr := Send(conn, srcPath)
	stdin.Close()

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case err := <-waitErr:
		if err != nil {
			t.Fatalf("rz exited with error: %v", err)
		}
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatal("rz did not exit in time")
	}

	if sendErr != nil {
		t.Fatalf("Send: %v", sendErr)
	}

	got, err := os.ReadFile(filepath.Join(dir, wantFilename))
	if err != nil {
		t.Fatalf("reading file rz received: %v", err)
	}
	return got
}

// flakyWriter accepts only its first allow bytes, then fails every
// subsequent Write with io.ErrClosedPipe -- standing in for sz's
// stdin pipe once cmd.Wait has auto-closed it.
type flakyWriter struct {
	allow int
	got   bytes.Buffer
}

func (w *flakyWriter) Write(p []byte) (int, error) {
	n := len(p)
	if n > w.allow {
		n = w.allow
	}
	w.got.Write(p[:n])
	w.allow -= n
	if n < len(p) {
		return n, io.ErrClosedPipe
	}
	return n, nil
}

func (w *flakyWriter) Close() error { return nil }

// TestCopyUntilClosedReturnsTheUnwrittenTail is a regression test for
// the actual bug found live: piping conn directly into sz's stdin
// (cmd.Stdin = conn) meant a byte conn had already produced -- the
// caller's own next keystroke, arriving right as sz exits and stops
// wanting input -- got silently dropped by plain io.Copy once the
// write to the now-closed pipe failed, costing the caller's first
// keystroke or two after every single transfer. copyUntilClosed must
// hand back exactly the bytes it couldn't deliver, not the whole read
// (including whatever prefix *did* get written, on a partial write)
// and not nothing.
func TestCopyUntilClosedReturnsTheUnwrittenTail(t *testing.T) {
	src := bytes.NewReader([]byte("helloX"))
	dst := &flakyWriter{allow: 5}

	got := copyUntilClosed(dst, src)

	if dst.got.String() != "hello" {
		t.Fatalf("delivered to dst = %q, want %q", dst.got.String(), "hello")
	}
	if string(got) != "X" {
		t.Fatalf("leftover = %q, want %q", got, "X")
	}
}

// TestCopyUntilClosedReturnsNilWhenSrcJustEndsCleanly confirms the
// ordinary case (src runs out with nothing left undelivered) reports
// no leftover at all, not an empty-but-non-nil slice a caller might
// mistakenly treat as "something to push back".
func TestCopyUntilClosedReturnsNilWhenSrcJustEndsCleanly(t *testing.T) {
	src := bytes.NewReader([]byte("hello"))
	dst := &flakyWriter{allow: 100}

	got := copyUntilClosed(dst, src)

	if got != nil {
		t.Fatalf("leftover = %q, want nil", got)
	}
	if dst.got.String() != "hello" {
		t.Fatalf("delivered to dst = %q, want %q", dst.got.String(), "hello")
	}
}
