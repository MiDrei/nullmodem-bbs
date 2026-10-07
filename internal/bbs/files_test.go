package bbs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/file"
	"github.com/midrei/nullmodem-bbs/internal/user"
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
	if _, err := s.Files.CreateArea("second", "Second Area", "", "", 0, 0); err != nil {
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

func TestFileAreasLightbarGroupsByNetwork(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := s.Files.CreateArea("fido", "Fido Files", "", "FidoNet", 0, 0); err != nil {
		t.Fatalf("CreateArea fido: %v", err)
	}
	if _, err := s.Files.CreateArea("fsx", "Fsx Files", "", "fsxNet", 0, 0); err != nil {
		t.Fatalf("CreateArea fsx: %v", err)
	}

	conn := newFakeConn("F\r\nQ\r\nQ\r\n")
	term := NewTerminal(conn)
	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()

	// Areas sort network ("" first, byte order thereafter), sort_order,
	// name: the seeded local "General Files" (no network), then
	// "FidoNet"'s divider and area, then "fsxNet"'s divider and area.
	generalIdx := strings.Index(out, "General Files")
	fidoDividerIdx := strings.Index(out, "FidoNet")
	fidoAreaIdx := strings.Index(out, "Fido Files")
	fsxDividerIdx := strings.Index(out, "fsxNet")
	fsxAreaIdx := strings.Index(out, "Fsx Files")
	if generalIdx < 0 || fidoDividerIdx < 0 || fidoAreaIdx < 0 || fsxDividerIdx < 0 || fsxAreaIdx < 0 {
		t.Fatalf("expected local area, both network dividers, and both network areas present, got: %q", out)
	}
	if !(generalIdx < fidoDividerIdx && fidoDividerIdx < fidoAreaIdx && fidoAreaIdx < fsxDividerIdx && fsxDividerIdx < fsxAreaIdx) {
		t.Fatalf("expected order General Files < FidoNet divider < Fido Files < fsxNet divider < Fsx Files, got: %q", out)
	}
	if strings.Count(out, "FidoNet") != 1 {
		t.Fatalf(`expected exactly one "FidoNet" divider, got %d: %q`, strings.Count(out, "FidoNet"), out)
	}
	if strings.Count(out, "fsxNet") != 1 {
		t.Fatalf(`expected exactly one "fsxNet" divider, got %d: %q`, strings.Count(out, "fsxNet"), out)
	}
}

func TestFileAreasLightbarNewCountUnaffectedByJustVisitingList(t *testing.T) {
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

	// Enter the area and immediately leave again WITHOUT opening the
	// file -- merely visiting the file list must not clear the area
	// lightbar's New count; only actually viewing a file does (see
	// TestFileAreasLightbarNewCountClearsAfterReadingFile).
	conn := newFakeConn("F\r\n\r\nQQQ\r\n")
	term := NewTerminal(conn)
	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	renders := strings.Split(conn.out.String(), hintMarker("[Up/Down] Move   [Enter] Select   [N] New files   [S] Search   [Q] Back"))
	if len(renders) < 3 {
		t.Fatalf("expected at least two lightbar redraws, got %d: %q", len(renders)-1, conn.out.String())
	}
	if !strings.Contains(renders[0], "     1      1      1") {
		t.Fatalf("expected New=1 before visiting the area, got: %q", renders[0])
	}
	if !strings.Contains(renders[1], "     1      1      1") {
		t.Fatalf("expected New still 1 after just visiting the list without viewing, got: %q", renders[1])
	}
}

func TestFileAreasLightbarNewCountClearsAfterReadingFile(t *testing.T) {
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

	// Enter the area, open the file in the reader (Enter on the file
	// list's only, already-highlighted row), leave, and check the area
	// lightbar's second draw shows New=0.
	conn := newFakeConn("F\r\n\r\n\r\nQQQQ\r\n")
	term := NewTerminal(conn)
	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	renders := strings.Split(conn.out.String(), hintMarker("[Up/Down] Move   [Enter] Select   [N] New files   [S] Search   [Q] Back"))
	if len(renders) < 3 {
		t.Fatalf("expected at least two lightbar redraws, got %d: %q", len(renders)-1, conn.out.String())
	}
	if !strings.Contains(renders[0], "     1      1      1") {
		t.Fatalf("expected New=1 before visiting the area, got: %q", renders[0])
	}
	if !strings.Contains(renders[1], "     1      0      1") {
		t.Fatalf("expected New=0 after reading the file, got: %q", renders[1])
	}
}

func TestFileListLightbarShowsNewFlagUntilActuallyRead(t *testing.T) {
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

	// Visit the file list twice (Enter the area, Q back out, Enter
	// again) WITHOUT opening the file -- it must stay flagged NEW both
	// times. Only the third visit, where Enter opens the file in the
	// reader, actually marks it read; the list's next redraw (after
	// backing out of the reader) must no longer flag it.
	conn := newFakeConn("F\r\n\r\nQ\r\n\r\nQQQQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	renders := strings.Split(out, hintMarker("[Up/Down] Move   [Enter] View   [D] Download   [U] Upload   [Q] Back"))
	if len(renders) < 4 {
		t.Fatalf("expected at least three file-list redraws, got %d: %q", len(renders)-1, out)
	}
	if !strings.Contains(renders[0], "NEW") {
		t.Fatalf("expected the file flagged NEW on the first visit, got: %q", renders[0])
	}
	if !strings.Contains(renders[1], "NEW") {
		t.Fatalf("expected the file still flagged NEW on the second visit (not yet read), got: %q", renders[1])
	}
	if strings.Contains(renders[2], "NEW") {
		t.Fatalf("expected the NEW flag gone after actually reading the file, got: %q", renders[2])
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
	if !strings.Contains(out, "\x1b[47m\x1b[30mNEW beta.txt") {
		t.Fatalf("expected beta.txt's row highlighted, got: %q", out)
	}
	readerRenders := strings.Split(out, hintMarker("[N/Right] Next  [P/Left] Prev  [Up/Dn] Scroll  [D] Download  [Q] Back to list"))
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
	// file-list lightbar's first row -> read alpha.txt, Right arrow ->
	// Next (beta.txt) without returning to the list, Left arrow -> Prev
	// (alpha.txt) again, then Q/Q/Q/Q to unwind back to a logoff.
	// (Up/Down are body-scroll now, not file switching -- see
	// TestReadFileArrowsScrollDescriptionInsteadOfSwitchingFiles.)
	conn := newFakeConn("F\r\n\r\n\r\n\x1b[C\x1b[DQQQQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	renders := strings.Split(out, hintMarker("[N/Right] Next  [P/Left] Prev  [Up/Dn] Scroll  [D] Download  [Q] Back to list"))
	if len(renders) < 4 {
		t.Fatalf("expected at least 3 reader redraws (initial, next, prev), got %d: %q", len(renders)-1, out)
	}
	if !strings.Contains(renders[0], "alpha.txt") {
		t.Fatalf("expected alpha.txt shown initially, got: %q", renders[0])
	}
	if !strings.Contains(renders[1], "beta.txt") {
		t.Fatalf("expected Right arrow to advance to beta.txt, got: %q", renders[1])
	}
	if !strings.Contains(renders[2], "alpha.txt") {
		t.Fatalf("expected Left arrow to return to alpha.txt, got: %q", renders[2])
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
	conn := newFakeConn("F\r\n\r\n\r\n\x1b[D\x1b[C\x1b[CQQQQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	renders := strings.Split(out, hintMarker("[N/Right] Next  [P/Left] Prev  [Up/Dn] Scroll  [D] Download  [Q] Back to list"))
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

func TestFirstUnreadFileIndexReturnsFirstUnread(t *testing.T) {
	files := []file.File{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}}
	readIDs := map[int64]bool{1: true, 2: true}
	if got := firstUnreadFileIndex(files, readIDs); got != 2 {
		t.Fatalf("firstUnreadFileIndex = %d, want 2 (file ID 3, the first unread)", got)
	}
}

func TestFirstUnreadFileIndexFallsBackToLastWhenAllRead(t *testing.T) {
	files := []file.File{{ID: 1}, {ID: 2}, {ID: 3}}
	readIDs := map[int64]bool{1: true, 2: true, 3: true}
	if got := firstUnreadFileIndex(files, readIDs); got != 2 {
		t.Fatalf("firstUnreadFileIndex = %d, want 2 (the last file, everything already read)", got)
	}
}

// TestFileListScrollsAndKeepsHeaderVisibleWithManyFiles mirrors
// messages.go's TestMessageListScrollsAndKeepsHeaderVisibleWithManyMessages:
// a long file list used to just dump every row in one shot, pushing
// the header off the top of the screen exactly the way an unpaginated
// message list once did.
func TestFileListScrollsAndKeepsHeaderVisibleWithManyFiles(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	general, err := s.Files.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	for i := 1; i <= 40; i++ {
		name := fmt.Sprintf("file%02d.txt", i)
		if _, err := s.Files.UploadFile(general.ID, u.ID, name, "", strings.NewReader("x")); err != nil {
			t.Fatalf("UploadFile %d: %v", i, err)
		}
	}

	conn := newFakeConn("F\r\n\r\nQQQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	renders := strings.Split(out, hintMarker("[Up/Down] Move   [Enter] View   [D] Download   [U] Upload   [Q] Back"))
	if len(renders) < 2 {
		t.Fatalf("expected at least one file-list redraw, got: %q", out)
	}
	firstRender := renders[0]
	if !strings.Contains(firstRender, "General Files") {
		t.Fatalf("expected the header (area name) to stay visible with 40 files, got: %q", firstRender)
	}
	if !strings.Contains(firstRender, "file01.txt") {
		t.Fatalf("expected the first file to appear in the initial view, got: %q", firstRender)
	}
	if strings.Contains(firstRender, "file40.txt") {
		t.Fatalf("expected the last file NOT to be visible in the initial (unscrolled) view, got: %q", firstRender)
	}
	if !strings.Contains(plainText(firstRender), " 1-") {
		t.Fatalf("expected a scroll-position indicator since the list doesn't fit one screen, got: %q", firstRender)
	}
}

// TestFileListArrowKeysClampAtFirstAndLastInsteadOfWrapping mirrors
// messages.go's TestMessageListArrowKeysClampAtFirstAndLastInsteadOfWrapping:
// the file list used to wrap the highlight around with the modulo
// operator instead of clamping at the edges like every other lightbar
// in this project.
func TestFileListArrowKeysClampAtFirstAndLastInsteadOfWrapping(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	general, err := s.Files.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	for _, name := range []string{"first.txt", "second.txt", "third.txt"} {
		if _, err := s.Files.UploadFile(general.ID, u.ID, name, "", strings.NewReader("x")); err != nil {
			t.Fatalf("UploadFile: %v", err)
		}
	}
	readerFooter := hintMarker("[N/Right] Next  [P/Left] Prev  [Up/Dn] Scroll  [D] Download  [Q] Back to list")

	// Up at the very first file must stay put, not wrap to the last.
	conn := newFakeConn("F\r\n\r\n" + strings.Repeat("\x1b[A", 3) + "\r\nQQQQ\r\n")
	term := NewTerminal(conn)
	if err := s.runMenu(term, u, 1, "main"); !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	renders := strings.Split(conn.out.String(), readerFooter)
	if len(renders) < 2 {
		t.Fatalf("expected the reader to open, got: %q", conn.out.String())
	}
	if !strings.Contains(renders[0], "first.txt") {
		t.Fatalf("expected Up at the first file to stay on it (not wrap to the last), got: %q", renders[0])
	}

	// Down past the last file must stay put, not wrap to the first.
	conn2 := newFakeConn("F\r\n\r\n" + strings.Repeat("\x1b[B", 5) + "\r\nQQQQ\r\n")
	term2 := NewTerminal(conn2)
	if err := s.runMenu(term2, u, 1, "main"); !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	renders2 := strings.Split(conn2.out.String(), readerFooter)
	if len(renders2) < 2 {
		t.Fatalf("expected the reader to open, got: %q", conn2.out.String())
	}
	if !strings.Contains(renders2[0], "third.txt") {
		t.Fatalf("expected Down past the last file to stay on it (not wrap to the first), got: %q", renders2[0])
	}
}

// TestFileListFooterAnchoredRegardlessOfFileCount mirrors messages.go's
// TestMessageListFooterAnchoredRegardlessOfMessageCount: the footer
// hint used to trail right after the last file row, so it landed on a
// different line depending on how many files happened to be in the
// area. It must always land on the same line -- short lists are
// padded with blank rows so the footer stays anchored at a consistent
// position.
func TestFileListFooterAnchoredRegardlessOfFileCount(t *testing.T) {
	renderWithNFiles := func(t *testing.T, n int) string {
		t.Helper()
		s := testServer(t)
		u, err := s.Users.Register("alice", "password123", user.SLNewUser)
		if err != nil {
			t.Fatalf("Register: %v", err)
		}
		general, err := s.Files.AreaByTag("general")
		if err != nil {
			t.Fatalf("AreaByTag: %v", err)
		}
		for i := 1; i <= n; i++ {
			name := fmt.Sprintf("file%02d.txt", i)
			if _, err := s.Files.UploadFile(general.ID, u.ID, name, "", strings.NewReader("x")); err != nil {
				t.Fatalf("UploadFile %d: %v", i, err)
			}
		}

		conn := newFakeConn("F\r\n\r\nQQQ\r\n")
		term := NewTerminal(conn)
		if err := s.runMenu(term, u, 1, "main"); !errors.Is(err, errLogoff) {
			t.Fatalf("runMenu error = %v, want errLogoff", err)
		}
		out := conn.out.String()
		colIdx := strings.Index(out, "Filename")
		hintIdx := strings.Index(out, hintMarker("[Up/Down] Move   [Enter] View   [D] Download   [U] Upload   [Q] Back"))
		if colIdx < 0 || hintIdx < 0 || hintIdx < colIdx {
			t.Fatalf("expected both the column header and the hint line to appear in order, got: %q", out)
		}
		return out[colIdx:hintIdx]
	}

	between1 := renderWithNFiles(t, 1)
	between3 := renderWithNFiles(t, 3)

	lines1 := strings.Count(between1, "\r\n")
	lines3 := strings.Count(between3, "\r\n")
	if lines1 != lines3 {
		t.Fatalf("lines between column header and footer hint = %d (1 file) vs %d (3 files), want equal -- the footer should be anchored, not trailing right after the last row", lines1, lines3)
	}
}

// TestReadFileArrowsScrollDescriptionInsteadOfSwitchingFiles mirrors
// messages.go's TestReadMessageArrowsScrollBodyInsteadOfSwitchingMessages:
// a long description that doesn't fit the screen used to have no way
// to scroll at all (readFile only supported switching files). Up/Down
// must now scroll within the current file's description -- clamped at
// the top/bottom, never switching to a different file on their own,
// since only N/P/Left/Right/Enter are supposed to do that.
func TestReadFileArrowsScrollDescriptionInsteadOfSwitchingFiles(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	general, err := s.Files.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	// Comfortably more lines than a 24-row terminal's reader viewport
	// (header + meta + footer eat a chunk of it too) can show at once.
	var descLines []string
	for i := 1; i <= 40; i++ {
		descLines = append(descLines, fmt.Sprintf("desc line %d", i))
	}
	description := strings.Join(descLines, "\n")
	if _, err := s.Files.UploadFile(general.ID, u.ID, "long.txt", description, strings.NewReader("x")); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	// Open the reader on the only file, scroll down twice, then back up
	// twice, then quit out without ever pressing N/P/arrow-left/arrow-right.
	conn := newFakeConn("F\r\n\r\n\r\n\x1b[B\x1b[B\x1b[A\x1b[AQQQQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()

	if !strings.Contains(out, "desc line 1\r\n") {
		t.Fatalf("expected the description's first line to appear in the initial view, got: %q", out)
	}
	if !strings.Contains(out, "line 1-") {
		t.Fatalf("expected a scroll-position hint since the description doesn't fit one screen, got: %q", out)
	}
	if strings.Contains(out, "desc line 40") {
		t.Fatalf("expected the last line NOT to be visible yet (only scrolled down twice), got: %q", out)
	}

	renders := strings.Split(out, hintMarker("[Up/Dn] Scroll  [D] Download  [Q] Back to list"))
	if len(renders) < 6 {
		t.Fatalf("expected at least 5 reader redraws (initial + 2 down + 2 up), got %d: %q", len(renders)-1, out)
	}
	for i, r := range renders[:5] {
		if !strings.Contains(r, "long.txt") {
			t.Fatalf("redraw %d left the reader (or switched files) unexpectedly, got: %q", i, r)
		}
	}
}

// TestReadFileResolvesANSICursorPositioningViaGrid mirrors messages.go's
// TestReadMessageResolvesANSICursorPositioningViaGrid: a description
// containing real ANSI escape sequences (someone pasting ANSI art into
// a file's description) must be resolved against a virtual canvas
// (ansi.ParseGrid) rather than word-wrapped, which would corrupt the
// color codes and cursor positioning alike.
func TestReadFileResolvesANSICursorPositioningViaGrid(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	general, err := s.Files.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	description := "\x1b[1;33mColorful\x1b[0m\n\x1b[s\n\x1b[uPositioned"
	if _, err := s.Files.UploadFile(general.ID, u.ID, "ansi.txt", description, strings.NewReader("x")); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	conn := newFakeConn("F\r\n\r\n\r\nQQQQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	colorfulIdx := strings.Index(out, "Colorful")
	positionedIdx := strings.Index(out, "Positioned")
	if colorfulIdx < 0 || positionedIdx < 0 {
		t.Fatalf("expected both %q and %q to appear in the reader output, got: %q", "Colorful", "Positioned", out)
	}
	between := out[colorfulIdx:positionedIdx]
	if strings.Count(between, "\r\n") != 1 {
		t.Fatalf("expected exactly one line break between Colorful and Positioned (cursor save/restore resolved onto the same row), got %d in %q", strings.Count(between, "\r\n"), between)
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
	// <path>, description, Enter past the result, M -> back to main, F -> file areas
	// lightbar, Enter -> General Files (the only area, already
	// highlighted), Enter again on the file-list lightbar's only row
	// -> file reader, then Q out of the reader, Q out of the file
	// list, Q out of the area lightbar, Q to log off from main.
	input := "S\r\nI\r\n1\r\n" + src + "\r\nA readme file\r\n\r\nM\r\nF\r\n\r\n\r\nQQQQ\r\n"
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

	input := "S\r\nA\r\ndoors\r\nDoor Games\r\nDOS door games\r\nfsxNet\r\n0\r\n0\r\nM\r\nQ\r\n"
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
	if area.Network != "fsxNet" {
		t.Fatalf("area.Network = %q, want %q", area.Network, "fsxNet")
	}
}
