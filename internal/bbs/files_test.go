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
	// should highlight and then open the second one. browseFileArea's
	// own file-list lightbar and the outer area lightbar are both
	// single-keystroke ReadKey loops, so exiting each with Q sends a
	// bare byte -- only the final Q (back at the ReadLine-based main
	// menu) needs its own trailing CRLF.
	conn := newFakeConn("F\r\n\x1b[B\r\nQQQ\r\n")
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

	conn := newFakeConn("F\r\n\r\nQQQ\r\n")
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

func TestFileListLightbarArrowNavigationSelectsSecondFile(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	general, err := s.Files.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if _, err := s.Files.UploadFile(general.ID, u.ID, "alpha.txt", "", strings.NewReader("a")); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if _, err := s.Files.UploadFile(general.ID, u.ID, "beta.txt", "", strings.NewReader("b")); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	// F -> areas lightbar, Enter -> General Files, one Down arrow
	// highlights the second file in the file-list lightbar, then
	// Enter opens the reader on it.
	conn := newFakeConn("F\r\n\r\n\x1b[B\r\nQQQQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "\x1b[47m\x1b[30mbeta.txt") {
		t.Fatalf("expected beta.txt's row highlighted, got: %q", out)
	}
	readerRenders := strings.Split(out, "[Enter/Dn/Right] Next  [Up/Left] Prev  [Q] Back to list")
	if len(readerRenders) < 2 {
		t.Fatalf("expected the reader to open, got: %q", out)
	}
	if !strings.Contains(readerRenders[0], "beta.txt") {
		t.Fatalf("expected Down arrow to open the second file, got: %q", readerRenders[0])
	}
}

func TestFileListLightbarUsesCustomRowTemplatesWhenPresent(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	dir := t.TempDir()
	s.ScreensDir = dir
	writeFile := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	writeFile("fillist-columns.ans", "CUSTOM-FILE-HEADER")
	writeFile("fillist-row.ans", ">> {FILENAME:-10}|{BY}")
	writeFile("fillist-row-selected.ans", "** {FILENAME:-10}|{BY}")

	general, err := s.Files.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if _, err := s.Files.UploadFile(general.ID, u.ID, "notes.txt", "", strings.NewReader("hi")); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	conn := newFakeConn("F\r\n\r\nQQQ\r\n")
	term := NewTerminal(conn)
	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "CUSTOM-FILE-HEADER") {
		t.Fatalf("expected custom column header, got: %q", out)
	}
	// Only one file exists, so it's always the (selected) row.
	if !strings.Contains(out, "** notes.txt |alice") {
		t.Fatalf("expected custom selected-row template rendered with macros, got: %q", out)
	}
	if strings.Contains(out, ">> ") {
		t.Fatalf("unselected row template should not appear when there's only one file, got: %q", out)
	}
}

func TestReadFileNextPrevNavigatesWithoutReturningToList(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	general, err := s.Files.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if _, err := s.Files.UploadFile(general.ID, u.ID, "alpha.txt", "", strings.NewReader("a")); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if _, err := s.Files.UploadFile(general.ID, u.ID, "beta.txt", "", strings.NewReader("b")); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	// F -> areas lightbar, Enter -> General Files, Enter again on the
	// file-list lightbar's first row -> read alpha.txt, Down arrow ->
	// Next (beta.txt) without returning to the list, Up arrow -> Prev
	// (alpha.txt) again, then Q/Q/Q/Q to unwind back to a logoff.
	conn := newFakeConn("F\r\n\r\n\r\n\x1b[B\x1b[AQQQQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	renders := strings.Split(out, "[Enter/Dn/Right] Next  [Up/Left] Prev  [Q] Back to list")
	if len(renders) < 4 {
		t.Fatalf("expected at least 3 reader redraws (initial, next, prev), got %d: %q", len(renders)-1, out)
	}
	if !strings.Contains(renders[0], "alpha.txt") {
		t.Fatalf("expected alpha.txt shown initially, got: %q", renders[0])
	}
	if !strings.Contains(renders[1], "beta.txt") {
		t.Fatalf("expected Down arrow to advance to beta.txt, got: %q", renders[1])
	}
	if !strings.Contains(renders[2], "alpha.txt") {
		t.Fatalf("expected Up arrow to return to alpha.txt, got: %q", renders[2])
	}
}

func TestReadFileNextPrevClampAtEnds(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	general, err := s.Files.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if _, err := s.Files.UploadFile(general.ID, u.ID, "alpha.txt", "", strings.NewReader("a")); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if _, err := s.Files.UploadFile(general.ID, u.ID, "beta.txt", "", strings.NewReader("b")); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	// Open the reader on the first file and press Prev/Left -- it must
	// stay on alpha.txt instead of wrapping to beta.txt. Then advance
	// to the last file and press Next/Right again -- it must stay
	// there instead of wrapping back to alpha.txt.
	conn := newFakeConn("F\r\n\r\n\r\n\x1b[A\x1b[C\x1b[CQQQQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	renders := strings.Split(out, "[Enter/Dn/Right] Next  [Up/Left] Prev  [Q] Back to list")
	if len(renders) < 4 {
		t.Fatalf("expected at least 3 reader redraws (initial, after Prev, after Next), got %d: %q", len(renders)-1, out)
	}
	if !strings.Contains(renders[0], "alpha.txt") {
		t.Fatalf("expected alpha.txt shown initially, got: %q", renders[0])
	}
	if !strings.Contains(renders[1], "alpha.txt") {
		t.Fatalf("expected Prev at the first file to stay put, got: %q", renders[1])
	}
	if !strings.Contains(renders[2], "beta.txt") {
		t.Fatalf("expected Next to advance to the last file, got: %q", renders[2])
	}
	if !strings.Contains(renders[3], "beta.txt") {
		t.Fatalf("expected Next at the last file to stay put instead of wrapping, got: %q", renders[3])
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
	// highlighted), Enter again on the file-list lightbar's only row
	// -> file reader, then Q out of the reader, Q out of the file
	// list, Q out of the area lightbar, Q to log off from main.
	input := "S\r\nI\r\n1\r\n" + src + "\r\nA readme file\r\nM\r\nF\r\n\r\n\r\nQQQQ\r\n"
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
