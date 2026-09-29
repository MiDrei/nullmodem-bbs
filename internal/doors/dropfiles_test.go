package doors

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testSession() Session {
	return Session{
		RealName:        "Swiss Maik",
		Handle:          "SwissMaik",
		AccessLevel:     50,
		TimeLeftMinutes: 60,
		Node:            3,
		UserID:          7,
		TotalCalls:      12,
		LastCall:        time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC),
		BBSName:         "Maiks Place",
		SysopName:       "Mike Dreier",
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	s := string(data)
	if !strings.HasSuffix(s, "\r\n") {
		t.Fatalf("%s doesn't end in CRLF: %q", path, s)
	}
	return strings.Split(strings.TrimSuffix(s, "\r\n"), "\r\n")
}

func TestDoorSysIsTheFull52LineFormat(t *testing.T) {
	dir := t.TempDir()
	name, err := writeDropFile(dir, DropFileDoorSys, testSession())
	if err != nil {
		t.Fatal(err)
	}
	if name != "DOOR.SYS" {
		t.Fatalf("name = %q", name)
	}
	lines := readLines(t, filepath.Join(dir, name))
	if len(lines) != 52 {
		t.Fatalf("DOOR.SYS has %d lines, want 52", len(lines))
	}
	for i, want := range map[int]string{
		1: "COM1:", 4: "3", 10: "Swiss Maik", 15: "50", 16: "12",
		17: "09/20/26", 19: "60", 20: "GR", 26: "7", 35: "Mike Dreier", 36: "SwissMaik",
	} {
		if lines[i-1] != want {
			t.Errorf("DOOR.SYS line %d = %q, want %q", i, lines[i-1], want)
		}
	}
}

func TestDorInfoWritesNodeFileAndSplitsNames(t *testing.T) {
	dir := t.TempDir()
	name, err := writeDropFile(dir, DropFileDorInfo, testSession())
	if err != nil {
		t.Fatal(err)
	}
	if name != "DORINFO1.DEF" {
		t.Fatalf("name = %q", name)
	}
	lines := readLines(t, filepath.Join(dir, "DORINFO3.DEF"))
	want := []string{"Maiks Place", "Mike", "Dreier", "COM1", "38400 BAUD,N,8,1", "0",
		"Swiss", "Maik", "", "1", "50", "60", "-1"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("DORINFO3.DEF =\n%q\nwant\n%q", lines, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "DORINFO1.DEF")); err != nil {
		t.Fatalf("DORINFO1.DEF not written: %v", err)
	}
}

func TestDorInfoNamesNodesAboveNineWithLetters(t *testing.T) {
	for node, want := range map[int]string{1: "DORINFO1.DEF", 9: "DORINFO9.DEF", 10: "DORINFOA.DEF", 35: "DORINFOZ.DEF"} {
		if got := dorInfoName(node); got != want {
			t.Errorf("dorInfoName(%d) = %q, want %q", node, got, want)
		}
	}
}

func TestDoorFileSR(t *testing.T) {
	dir := t.TempDir()
	if _, err := writeDropFile(dir, DropFileDoorFileSR, testSession()); err != nil {
		t.Fatal(err)
	}
	lines := readLines(t, filepath.Join(dir, "DOORFILE.SR"))
	want := []string{"Swiss Maik", "1", "1", "24", "38400", "1", "60", "Swiss Maik"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("DOORFILE.SR = %q, want %q", lines, want)
	}
}

func TestExpandLaunchCmdFillsPlaceholdersPerLine(t *testing.T) {
	got := expandLaunchCmd("OOINFO.EXE 2 {dropfile_dir} {node}\r\n\r\n  OOII.EXE  \nDREDD /P{dropfile}", "DOOR.SYS", 4)
	want := "OOINFO.EXE 2 D:\\ 4\nOOII.EXE\nDREDD /PD:\\DOOR.SYS"
	if got != want {
		t.Fatalf("expandLaunchCmd = %q, want %q", got, want)
	}
}

func TestBuildDOSBoxCmdWritesDropFileIntoDoorDirWhenAsked(t *testing.T) {
	nodeDir, doorDir := t.TempDir(), t.TempDir()
	door := Door{
		Name: "lord", Kind: "dosbox", DOSBoxDir: doorDir,
		DOSBoxLaunchCmd: "CALL START.BAT {node}", DropFile: DropFileDorInfo, DropFileInDoorDir: true,
	}
	if _, err := buildDOSBoxCmd(nodeDir, door, testSession()); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		filepath.Join(nodeDir, "DORINFO1.DEF"),
		filepath.Join(doorDir, "DORINFO1.DEF"),
		filepath.Join(doorDir, "DORINFO3.DEF"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s: %v", p, err)
		}
	}
	conf, _ := os.ReadFile(filepath.Join(nodeDir, "dosbox.conf"))
	if !strings.Contains(string(conf), "CALL START.BAT 3\nEXIT") {
		t.Fatalf("launch command not expanded:\n%s", conf)
	}
}

func TestAcquireClearsLockFilesOnlyForTheFirstSession(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "OONODE.DAT")
	door := Door{Name: "oo2-test", Kind: "dosbox", DOSBoxDir: dir, LockFiles: []string{"oonode.dat"}}

	os.WriteFile(lock, []byte("x"), 0o644)
	release1, err := acquire(door)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("lock file should be gone for the first session, stat err = %v", err)
	}

	// Another node is now in the game and holds a real lock.
	os.WriteFile(lock, []byte("x"), 0o644)
	release2, err := acquire(door)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("lock file must survive while someone is playing: %v", err)
	}
	release2()
	release1()

	release3, err := acquire(door)
	if err != nil {
		t.Fatal(err)
	}
	release3()
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("stale lock should be cleared once nobody plays, stat err = %v", err)
	}
}

func TestAcquireRejectsLockFilesOutsideTheDoorDir(t *testing.T) {
	door := Door{Name: "bad", Kind: "dosbox", DOSBoxDir: t.TempDir(), LockFiles: []string{"../etc/passwd"}}
	if _, err := acquire(door); err == nil {
		t.Fatal("acquire accepted a lock file outside the door dir")
	}
}

func TestValidateDropFileRejectsUnknownFormats(t *testing.T) {
	if err := validateDropFile(Door{Name: "x", DropFile: "chain.txt"}); err == nil {
		t.Fatal("unknown format accepted")
	}
	for _, f := range append([]string{""}, DropFileFormats...) {
		if err := validateDropFile(Door{Name: "x", DropFile: f}); err != nil {
			t.Errorf("format %q rejected: %v", f, err)
		}
	}
}

func TestNativeArgsPlaceholdersOrLegacySwitch(t *testing.T) {
	got := nativeArgs([]string{"-dropfile", "{dropfile}", "-data", "data", "-n", "{node}"}, "/tmp/n", 2, "")
	want := []string{"-dropfile", "/tmp/n/DOOR32.SYS", "-data", "data", "-n", "2"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("nativeArgs = %q, want %q", got, want)
	}
	legacy := nativeArgs([]string{"-x"}, "/tmp/n", 1, "")
	if strings.Join(legacy, " ") != "-x /P/tmp/n/" {
		t.Fatalf("legacy nativeArgs = %q", legacy)
	}
	withIP := nativeArgs([]string{"-D", "{dropfile}", "-IP{ip}"}, "/tmp/n", 1, "192.0.2.7")
	if strings.Join(withIP, " ") != "-D /tmp/n/DOOR32.SYS -IP192.0.2.7" {
		t.Fatalf("nativeArgs with ip = %q", withIP)
	}
	// Unknown IP: the argument is left out, not passed as a bare "-IP".
	noIP := nativeArgs([]string{"-D", "{dropfile}", "-IP{ip}"}, "/tmp/n", 1, "")
	if strings.Join(noIP, " ") != "-D /tmp/n/DOOR32.SYS" {
		t.Fatalf("nativeArgs without ip = %q", noIP)
	}
}
