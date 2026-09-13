package bbs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

func writeTempUploadFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "readme.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp upload file: %v", err)
	}
	return path
}

func TestFileAreasListSeededArea(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	conn := newFakeConn("F\r\nQ\r\nQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), "General Files") {
		t.Fatalf("area list missing seeded file area: %q", conn.out.String())
	}
}

func TestFileAreasUsesCustomHeaderScreenWhenPresent(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	dir := t.TempDir()
	s.ScreensDir = dir
	if err := os.WriteFile(filepath.Join(dir, "filareas.ans"), []byte("File Areas at {BBSNAME}"), 0o644); err != nil {
		t.Fatalf("write filareas.ans: %v", err)
	}
	s.BBSName = "Test BBS"

	conn := newFakeConn("F\r\nQ\r\nQ\r\n")
	term := NewTerminal(conn)
	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "File Areas at Test BBS") {
		t.Fatalf("expected rendered custom header, got: %q", out)
	}
}

func TestFileAreasFallsBackToPlainTitleWhenHeaderScreenMissing(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	s.ScreensDir = t.TempDir()

	conn := newFakeConn("F\r\nQ\r\nQ\r\n")
	term := NewTerminal(conn)
	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), "File Areas") {
		t.Fatalf("expected fallback plain title, got: %q", conn.out.String())
	}
}

func TestFileAreasLightbarShowsCounts(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	general, err := s.Files.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if _, err := s.Files.UploadFile(general.ID, u.ID, "notes.txt", "", strings.NewReader("hi")); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	conn := newFakeConn("F\r\nQ\r\nQ\r\n")
	term := NewTerminal(conn)
	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "Total") || !strings.Contains(out, "New") || !strings.Contains(out, "Yours") {
		t.Fatalf("expected a Total/New/Yours header, got: %q", out)
	}
	if !strings.Contains(out, "General Files") || !strings.Contains(out, "     1      1      1") {
		t.Fatalf("expected counts 1/1/1 for General Files, got: %q", out)
	}
}

func TestFileAreasLightbarArrowNavigationSelectsSecondArea(t *testing.T) {
	s := testServer(t)
	if _, err := s.Files.CreateArea("second", "Second Area", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// "General Files" sorts before "Second Area"; one Down arrow
	// should highlight and then open the second one.
	conn := newFakeConn("F\r\n\x1b[B\r\nQ\r\nQ\r\nQ\r\n")
	term := NewTerminal(conn)
	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), "\x1b[1;36mSecond Area\x1b[0m") {
		t.Fatalf("expected to have entered Second Area, got: %q", conn.out.String())
	}
}

func TestFileAreasLightbarMarksAreaReadOnEnter(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	general, err := s.Files.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if _, err := s.Files.UploadFile(general.ID, u.ID, "notes.txt", "", strings.NewReader("hi")); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	conn := newFakeConn("F\r\n\r\nQ\r\nQ\r\nQ\r\n")
	term := NewTerminal(conn)
	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	renders := strings.Split(conn.out.String(), "[Up/Down] Move   [Enter] Select   [Q] Back")
	if len(renders) < 3 {
		t.Fatalf("expected at least two lightbar redraws, got %d: %q", len(renders)-1, conn.out.String())
	}
	if !strings.Contains(renders[0], "     1      1      1") {
		t.Fatalf("expected New=1 before visiting the area, got: %q", renders[0])
	}
	if !strings.Contains(renders[1], "     1      0      1") {
		t.Fatalf("expected New=0 after visiting the area, got: %q", renders[1])
	}
}

func TestSysopImportAndBrowseFile(t *testing.T) {
	s := testServer(t)
	sysop, err := s.Users.Register("root", "password123", user.SLSysop)
	if err != nil {
		t.Fatalf("Register sysop: %v", err)
	}
	src := writeTempUploadFile(t, "hello file area")

	// S -> sysop menu, I -> import file, "1" -> General Files,
	// <path>, description, M -> back to main, F -> file areas
	// lightbar, Enter -> General Files (the only area, already
	// highlighted), "1" -> file details (still numeric within an
	// area's own file list), Q, Q, Q.
	input := "S\r\nI\r\n1\r\n" + src + "\r\nA readme file\r\nM\r\nF\r\n\r\n1\r\nQ\r\nQ\r\nQ\r\n"
	conn := newFakeConn(input)
	term := NewTerminal(conn)

	err = s.runMenu(term, sysop, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "Imported readme.txt") {
		t.Fatalf("expected import confirmation, got: %q", out)
	}
	if !strings.Contains(out, "readme.txt") || !strings.Contains(out, "A readme file") {
		t.Fatalf("expected file listing/details, got: %q", out)
	}
	if !strings.Contains(out, "Uploaded:") {
		t.Fatalf("expected file details view, got: %q", out)
	}
}

func TestSysopImportRejectsMissingSource(t *testing.T) {
	s := testServer(t)
	sysop, err := s.Users.Register("root", "password123", user.SLSysop)
	if err != nil {
		t.Fatalf("Register sysop: %v", err)
	}

	input := "S\r\nI\r\n1\r\n/no/such/file.txt\r\ndesc\r\nM\r\nQ\r\n"
	conn := newFakeConn(input)
	term := NewTerminal(conn)

	err = s.runMenu(term, sysop, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), "Import failed") {
		t.Fatalf("expected import failure message, got: %q", conn.out.String())
	}
}

func TestSysopCreateFileArea(t *testing.T) {
	s := testServer(t)
	sysop, err := s.Users.Register("root", "password123", user.SLSysop)
	if err != nil {
		t.Fatalf("Register sysop: %v", err)
	}

	input := "S\r\nA\r\ndoors\r\nDoor Games\r\nDOS door games\r\n0\r\n0\r\nM\r\nQ\r\n"
	conn := newFakeConn(input)
	term := NewTerminal(conn)

	err = s.runMenu(term, sysop, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), `Area "Door Games" created.`) {
		t.Fatalf("expected creation confirmation, got: %q", conn.out.String())
	}

	area, err := s.Files.AreaByTag("doors")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if area.Name != "Door Games" {
		t.Fatalf("area.Name = %q, want %q", area.Name, "Door Games")
	}
}
