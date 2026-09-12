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
	s := testServerWithUsers(t)
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

func TestSysopImportAndBrowseFile(t *testing.T) {
	s := testServerWithUsers(t)
	sysop, err := s.Users.Register("root", "password123", user.SLSysop)
	if err != nil {
		t.Fatalf("Register sysop: %v", err)
	}
	src := writeTempUploadFile(t, "hello file area")

	// S -> sysop menu, I -> import file, "1" -> General Files,
	// <path>, description, M -> back to main, F -> file areas,
	// "1" -> General Files, "1" -> file details, Q, Q, Q.
	input := "S\r\nI\r\n1\r\n" + src + "\r\nA readme file\r\nM\r\nF\r\n1\r\n1\r\nQ\r\nQ\r\nQ\r\n"
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
	s := testServerWithUsers(t)
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
	s := testServerWithUsers(t)
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
