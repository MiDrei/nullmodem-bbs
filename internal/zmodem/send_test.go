package zmodem

import (
	"bytes"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// rwPair adapts a separate reader and writer (rz's stdout/stdin
// pipes) into the single io.ReadWriter Send expects, matching what a
// real telnet/SSH connection's raw byte stream looks like.
type rwPair struct {
	io.Reader
	io.Writer
}

// requireRZ skips the test if the real lrzsz "rz" binary isn't
// installed -- these tests are interop checks against a real Zmodem
// receiver, not just this package's own code, so there's no
// meaningful fallback without it.
func requireRZ(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rz"); err != nil {
		t.Skip("rz (lrzsz) not installed; skipping real-interop Zmodem test")
	}
}

// sendAndReceive runs Send against a real "rz" subprocess (writing
// into a fresh temp directory) and returns the bytes rz actually
// wrote to disk for filename, or fails the test.
func sendAndReceive(t *testing.T, filename string, content []byte) []byte {
	t.Helper()
	requireRZ(t)

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
	sendErr := Send(conn, bytes.NewReader(content), filename, int64(len(content)), time.Now())
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

	got, err := os.ReadFile(filepath.Join(dir, filename))
	if err != nil {
		t.Fatalf("reading file rz received: %v", err)
	}
	return got
}

func TestSendSmallFileRoundTripsThroughRealRZ(t *testing.T) {
	content := []byte("hello zmodem world\n")
	got := sendAndReceive(t, "readme.txt", content)
	if !bytes.Equal(got, content) {
		t.Fatalf("received content = %q, want %q", got, content)
	}
}

func TestSendEmptyFileRoundTripsThroughRealRZ(t *testing.T) {
	got := sendAndReceive(t, "empty.txt", nil)
	if len(got) != 0 {
		t.Fatalf("received content = %q, want empty", got)
	}
}

func TestSendMultiBlockFileRoundTripsThroughRealRZ(t *testing.T) {
	// Bigger than blockSize so more than one data subpacket is
	// exercised, with deterministic pseudo-random bytes (including
	// values that need ZDLE-escaping, like 0x18 and 0x11) rather than
	// repetitive/text-like content that could hide an escaping bug.
	rng := rand.New(rand.NewSource(42))
	content := make([]byte, blockSize*3+777)
	if _, err := rng.Read(content); err != nil {
		t.Fatalf("generating random content: %v", err)
	}
	got := sendAndReceive(t, "bigfile.bin", content)
	if !bytes.Equal(got, content) {
		t.Fatalf("received %d bytes, want %d bytes; content mismatch", len(got), len(content))
	}
}

func TestSendContentWithEveryByteValueRoundTripsThroughRealRZ(t *testing.T) {
	// Every possible byte value 0-255, repeated a few times to also
	// cross a data-subpacket boundary -- the most direct possible
	// check that escaping/unescaping round-trips correctly for every
	// control byte this package treats specially (and every one it
	// deliberately doesn't).
	var content []byte
	for i := 0; i < 20; i++ {
		for b := 0; b < 256; b++ {
			content = append(content, byte(b))
		}
	}
	got := sendAndReceive(t, "allbytes.bin", content)
	if !bytes.Equal(got, content) {
		t.Fatalf("received %d bytes, want %d bytes; content mismatch (byte-value round-trip failed)", len(got), len(content))
	}
}
