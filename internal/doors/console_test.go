//go:build linux

package doors

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A door with Console gets a terminal on stdin and stdout (so it doesn't
// take its I/O for redirected) and still talks over the socket; without
// it, they aren't one.
func TestRunConsoleGivesTheDoorATerminal(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "door.sh")
	os.WriteFile(script, []byte("#!/bin/sh\nif [ -t 0 ] && [ -t 1 ]; then printf 'tty.' >&3; else printf 'notty.' >&3; fi\necho console-noise\n"), 0o755)

	for _, c := range []struct {
		console bool
		want    string
	}{{true, "tty."}, {false, "notty."}} {
		door := Door{Name: "tty", Exe: script, Dir: dir, Console: c.console}
		client, server := net.Pipe()
		done := make(chan error, 1)
		go func() { done <- Run(server, door, Session{Handle: "t", Node: 1}) }()
		client.SetReadDeadline(time.Now().Add(5 * time.Second))
		if got := readUntil(t, client, c.want); got != c.want {
			t.Errorf("console=%v: door said %q, want %q", c.console, got, c.want)
		}
		client.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("console=%v: Run did not return", c.console)
		}
	}
}
