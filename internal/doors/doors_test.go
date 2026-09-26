package doors

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain lets this test binary also act as a stand-in "door"
// process (the classic os/exec_test.go pattern: re-exec the test
// binary itself instead of needing a separate compiled fixture).
// Real doors (Usurper included) aren't part of this repo -- see
// docs/adding-a-door.md -- so exercising Run's actual socket/dropfile
// plumbing needs a door binary that has no external dependency at
// all.
func TestMain(m *testing.M) {
	if os.Getenv("DOORS_TEST_HELPER") == "1" {
		runHelperDoor()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runHelperDoor is the fake door: it reads whatever DOOR32.SYS it was
// handed at its own last argument's path (the "/P<dir>/" this
// package's Run always appends) to prove the dropfile was actually
// written and discoverable, then echoes every byte it receives on the
// inherited fd 3 back with an "echo:" prefix, until conn closes.
func runHelperDoor() {
	dropDir := os.Args[len(os.Args)-1]
	dropDir = dropDir[2 : len(dropDir)-1] // strip "/P" prefix and trailing "/"
	data, err := os.ReadFile(dropDir + "/DOOR32.SYS")
	if err != nil {
		os.Exit(2)
	}
	f := os.NewFile(3, "door")
	if _, err := f.Write(append([]byte("dropfile:"), data...)); err != nil {
		os.Exit(3)
	}
	buf := make([]byte, 4096)
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			if string(buf[:n]) == "QUIT" {
				// The stand-in door voluntarily exiting mid-session --
				// conn is still alive and the caller hasn't
				// necessarily typed anything since, exercising the
				// deadliner/shutdownWait path Run falls back to.
				return
			}
			if _, werr := f.Write(append([]byte("echo:"), buf[:n]...)); werr != nil {
				return
			}
		}
		if rerr != nil {
			return
		}
	}
}

func TestRunWritesDropFileAndBridgesBytesOverTheSocket(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	os.Setenv("DOORS_TEST_HELPER", "1")
	defer os.Unsetenv("DOORS_TEST_HELPER")

	door := Door{Name: "helper", Exe: exe, Dir: t.TempDir()}
	sess := Session{RealName: "Test User", Handle: "tester", AccessLevel: 42, TimeLeftMinutes: 30, Node: 1}

	client, serverSide := net.Pipe()
	defer client.Close()

	runErrCh := make(chan error, 1)
	go func() { runErrCh <- Run(serverSide, door, sess) }()

	client.SetReadDeadline(time.Now().Add(5 * time.Second))

	// The helper writes the dropfile contents (prefixed "dropfile:")
	// the instant it starts, before ever reading from us.
	dropfile := readUntil(t, client, "dropfile:")
	for _, want := range []string{"2\n", "3\n", "Test User\n", "tester\n", "42\n", "30\n"} {
		if !contains(dropfile, want) {
			t.Fatalf("DOOR32.SYS missing expected field %q, got: %q", want, dropfile)
		}
	}

	if _, err := client.Write([]byte("hello")); err != nil {
		t.Fatalf("write to door: %v", err)
	}
	echoed := readUntil(t, client, "echo:hello")
	if echoed != "echo:hello" {
		t.Fatalf("echoed = %q, want %q", echoed, "echo:hello")
	}

	// Closing the connection out from under a still-running door (the
	// caller disconnecting mid-game) has nothing graceful to do --
	// Run kills the door process rather than leaving it running with
	// nobody attached, so this always comes back as some non-nil
	// error. What actually matters here is that Run returns promptly
	// at all instead of hanging forever waiting for the door to
	// notice on its own (see Run's own doc comment on the race
	// between cmdDone/doorToConnDone/connToDoorDone for why that
	// cooperative-only approach turned out not to be reliable).
	client.Close()

	select {
	case <-runErrCh:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the connection closed")
	}
}

// TestRunReturnsCleanlyWhenTheDoorExitsOnItsOwn covers the normal end
// of a play session -- the caller quits the game from inside it,
// exiting the door process while conn itself is still perfectly
// alive. Run must still return promptly rather than blocking on
// connToDoorDone's still-pending Read(conn), which nothing the door
// did touches directly -- see Run's deadliner/shutdownWait fallback.
func TestRunReturnsCleanlyWhenTheDoorExitsOnItsOwn(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	os.Setenv("DOORS_TEST_HELPER", "1")
	defer os.Unsetenv("DOORS_TEST_HELPER")

	origWait := shutdownWait
	shutdownWait = 500 * time.Millisecond
	defer func() { shutdownWait = origWait }()

	door := Door{Name: "helper", Exe: exe, Dir: t.TempDir()}
	sess := Session{RealName: "Test User", Handle: "tester", AccessLevel: 1, TimeLeftMinutes: 30, Node: 1}

	client, serverSide := net.Pipe()
	defer client.Close()

	runErrCh := make(chan error, 1)
	go func() { runErrCh <- Run(serverSide, door, sess) }()

	client.SetReadDeadline(time.Now().Add(5 * time.Second))
	readUntil(t, client, "dropfile:")

	if _, err := client.Write([]byte("QUIT")); err != nil {
		t.Fatalf("write to door: %v", err)
	}

	select {
	case err := <-runErrCh:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the door exited on its own")
	}
}

// TestBuildDOSBoxCmdSubstitutesDropfileDirAndMounts locks in the
// generated per-session DOSBox-X config's shape: door.DOSBoxDir
// mounted as C:, the scratch node dir mounted as D:, and the
// "{dropfile_dir}" placeholder in DOSBoxLaunchCmd resolved to that
// same D: mount -- see buildDOSBoxCmd's doc comment for why (most
// classic doors take their drop file's directory via a command-line
// switch, e.g. DOORWAY's "/s:", rather than a fixed convention).
func TestBuildDOSBoxCmdSubstitutesDropfileDirAndMounts(t *testing.T) {
	nodeDir := t.TempDir()
	door := Door{
		Name:            "doorway",
		Kind:            "dosbox",
		DOSBoxDir:       "/opt/doors/doorway",
		DOSBoxLaunchCmd: `DOORWAY\DOORWAYU SYS /s:{dropfile_dir} /c:dos`,
	}
	sess := Session{RealName: "Test User", AccessLevel: 10, TimeLeftMinutes: 30, Node: 1}

	cmd, err := buildDOSBoxCmd(nodeDir, door, sess)
	if err != nil {
		t.Fatalf("buildDOSBoxCmd: %v", err)
	}
	if cmd.Path != "dosbox-x" && !strings.HasSuffix(cmd.Path, "/dosbox-x") {
		t.Fatalf("cmd.Path = %q, want dosbox-x", cmd.Path)
	}

	confPath := filepath.Join(nodeDir, "dosbox.conf")
	data, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("reading generated config: %v", err)
	}
	conf := string(data)

	if !strings.Contains(conf, "MOUNT C /opt/doors/doorway") {
		t.Fatalf("config missing door install dir mount, got:\n%s", conf)
	}
	if !strings.Contains(conf, "MOUNT D "+nodeDir) {
		t.Fatalf("config missing drop file dir mount, got:\n%s", conf)
	}
	if !strings.Contains(conf, `DOORWAY\DOORWAYU SYS /s:D:\ /c:dos`) {
		t.Fatalf("launch command's {dropfile_dir} placeholder wasn't substituted, got:\n%s", conf)
	}
	if !strings.Contains(conf, "inhsocket:1") {
		t.Fatalf("config missing inhsocket:1, got:\n%s", conf)
	}

	dropfile, err := os.ReadFile(filepath.Join(nodeDir, "DOOR.SYS"))
	if err != nil {
		t.Fatalf("reading DOOR.SYS: %v", err)
	}
	for _, want := range []string{"COM1:\r\n", "Test User\r\n", "10\r\n"} {
		if !strings.Contains(string(dropfile), want) {
			t.Fatalf("DOOR.SYS missing expected field %q, got: %q", want, dropfile)
		}
	}
}

func readUntil(t *testing.T, r io.Reader, want string) string {
	t.Helper()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for len(buf) < len(want) {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			t.Fatalf("reading: %v (got %q so far)", err, buf)
		}
	}
	return string(buf)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i+len(substr) <= len(s); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}
