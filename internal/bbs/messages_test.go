package bbs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

func TestMessageAreasListAndReadSeededArea(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	conn := newFakeConn("M\r\nQ\r\nQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "General Discussion") {
		t.Fatalf("area list missing seeded area: %q", out)
	}
}

func TestMessageAreasUsesCustomHeaderScreenWhenPresent(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	dir := t.TempDir()
	s.ScreensDir = dir
	if err := os.WriteFile(filepath.Join(dir, "msgareas.ans"), []byte("Msg Areas at {BBSNAME}"), 0o644); err != nil {
		t.Fatalf("write msgareas.ans: %v", err)
	}
	s.BBSName = "Test BBS"

	conn := newFakeConn("M\r\nQ\r\nQ\r\n")
	term := NewTerminal(conn)
	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "Msg Areas at Test BBS") {
		t.Fatalf("expected rendered custom header, got: %q", out)
	}
	if strings.Contains(out, "\x1b[1;36mMessage Areas") {
		t.Fatalf("custom header should replace the plain fallback title, got: %q", out)
	}
}

func TestMessageAreasLightbarShowsCounts(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	general, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if _, err := s.Messages.PostMessage(general.ID, u.ID, "All", "Hi", "hi"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	conn := newFakeConn("M\r\nQ\r\nQ\r\n")
	term := NewTerminal(conn)
	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "Total") || !strings.Contains(out, "New") || !strings.Contains(out, "Yours") {
		t.Fatalf("expected a Total/New/Yours header, got: %q", out)
	}
	// One message, posted by alice herself: Total=1, New=1 (never
	// visited yet), Yours=1.
	if !strings.Contains(out, "General Discussion") || !strings.Contains(out, "     1      1      1") {
		t.Fatalf("expected counts 1/1/1 for General Discussion, got: %q", out)
	}
}

func TestMessageAreasLightbarArrowNavigationSelectsSecondArea(t *testing.T) {
	s := testServer(t)
	if _, err := s.Messages.CreateArea("second", "Second Area", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// "General Discussion" sorts before "Second Area"; one Down arrow
	// should highlight and then open the second one. browseArea's own
	// message-list lightbar and the outer area lightbar are both
	// single-keystroke ReadKey loops now, so exiting each with Q sends
	// a bare byte -- only the final Q (back at the ReadLine-based main
	// menu) needs its own trailing CRLF.
	conn := newFakeConn("M\r\n\x1b[B\r\nQQQ\r\n")
	term := NewTerminal(conn)
	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	// browseArea prints the area's own name as a header once entered.
	if !strings.Contains(conn.out.String(), "\x1b[1;36mSecond Area\x1b[0m") {
		t.Fatalf("expected to have entered Second Area, got: %q", conn.out.String())
	}
}

func TestMessageAreasLightbarMarksAreaReadOnEnter(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	general, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if _, err := s.Messages.PostMessage(general.ID, u.ID, "All", "Hi", "hi"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	// Enter the area (marks it read), leave, and check the lightbar's
	// second draw shows New=0 instead of the initial New=1. Q exits
	// browseArea's own message-list lightbar as a bare keystroke, like
	// the outer area lightbar's Q -- only the final Q needs a CRLF, to
	// be read as a line by the main menu's ReadLine prompt.
	conn := newFakeConn("M\r\n\r\nQQQ\r\n")
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

func TestMessageAreasLightbarUsesCustomRowTemplatesWhenPresent(t *testing.T) {
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
	writeFile("msgareas-columns.ans", "CUSTOM-HEADER")
	writeFile("msgareas-row.ans", ">> {AREANAME:-10}|{TOTAL:3}|{NEWFLAG}")
	writeFile("msgareas-row-selected.ans", "** {AREANAME:-10}|{TOTAL:3}|{NEWFLAG}")

	general, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if _, err := s.Messages.PostMessage(general.ID, u.ID, "All", "Hi", "hi"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	conn := newFakeConn("M\r\nQ\r\nQ\r\n")
	term := NewTerminal(conn)
	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "CUSTOM-HEADER") {
		t.Fatalf("expected custom column header, got: %q", out)
	}
	// Only one area exists, so it's always the (selected) row.
	if !strings.Contains(out, "** General Di|  1|NEW") {
		t.Fatalf("expected custom selected-row template rendered with macros, got: %q", out)
	}
	if strings.Contains(out, ">> ") {
		t.Fatalf("unselected row template should not appear when there's only one area, got: %q", out)
	}
}

func TestMessageListLightbarArrowNavigationSelectsSecondMessage(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if _, err := s.Messages.PostMessage(area.ID, u.ID, "All", "First Subject", "first body"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.Messages.PostMessage(area.ID, u.ID, "All", "Second Subject", "second body"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	// M -> areas lightbar, Enter -> General Discussion, one Down arrow
	// highlights the second message in the message-list lightbar, then
	// Enter opens the reader on it. The reader's own footer only
	// appears once we've actually entered it, confirming the arrow
	// key moved the highlight before selection.
	conn := newFakeConn("M\r\n\r\n\x1b[B\r\nQQQQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "\x1b[47m\x1b[30mSecond Subject") {
		t.Fatalf("expected Second Subject's row highlighted, got: %q", out)
	}
	readerRenders := strings.Split(out, "[Enter/Dn/Right] Next  [Up/Left] Prev  [Q] Back to list")
	if len(readerRenders) < 2 {
		t.Fatalf("expected the reader to open, got: %q", out)
	}
	if !strings.Contains(readerRenders[0], "Second Subject") {
		t.Fatalf("expected Down arrow to open the second message, got: %q", readerRenders[0])
	}
}

func TestMessageListLightbarUsesCustomRowTemplatesWhenPresent(t *testing.T) {
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
	writeFile("msglist-columns.ans", "CUSTOM-MSG-HEADER")
	writeFile("msglist-row.ans", ">> {SUBJECT:-10}|{FROM}")
	writeFile("msglist-row-selected.ans", "** {SUBJECT:-10}|{FROM}")

	area, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if _, err := s.Messages.PostMessage(area.ID, u.ID, "All", "Hi There", "hi"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	conn := newFakeConn("M\r\n\r\nQQQ\r\n")
	term := NewTerminal(conn)
	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "CUSTOM-MSG-HEADER") {
		t.Fatalf("expected custom column header, got: %q", out)
	}
	// Only one message exists, so it's always the (selected) row.
	if !strings.Contains(out, "** Hi There  |alice") {
		t.Fatalf("expected custom selected-row template rendered with macros, got: %q", out)
	}
	if strings.Contains(out, ">> ") {
		t.Fatalf("unselected row template should not appear when there's only one message, got: %q", out)
	}
}

func TestPostAndReadMessage(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// M -> areas lightbar, Enter -> General Discussion (the only area,
	// already highlighted; still empty, so browseArea shows its empty-
	// list P/Q prompt), P -> post (a bare keystroke -- postMessage's
	// own subject/body prompts are ReadLine-based and need real CRLFs),
	// subject, two body lines, "." to end. browseArea's outer loop
	// refetches and now shows the message-list lightbar with the new
	// post highlighted; Enter opens the reader, Q backs out of the
	// reader, Q out of the message list, Q out of the area lightbar,
	// Q to log off from main.
	input := "M\r\n\r\nPHello World\r\nLine one\r\nLine two\r\n.\r\n\r\nQQQQ\r\n"
	conn := newFakeConn(input)
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "Message posted.") {
		t.Fatalf("expected posting confirmation, got: %q", out)
	}
	if !strings.Contains(out, "Hello World") {
		t.Fatalf("expected subject in listing/read view, got: %q", out)
	}
	if !strings.Contains(out, "Line one") || !strings.Contains(out, "Line two") {
		t.Fatalf("expected multi-line body in read view, got: %q", out)
	}
	if !strings.Contains(out, "From:    \x1b[1;37malice") {
		t.Fatalf("expected author in read view, got: %q", out)
	}
}

func TestReadMessageNextPrevNavigatesWithoutReturningToList(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if _, err := s.Messages.PostMessage(area.ID, u.ID, "All", "First Subject", "first body"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.Messages.PostMessage(area.ID, u.ID, "All", "Second Subject", "second body"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	// M -> areas lightbar, Enter -> General Discussion, Enter again on
	// the message-list lightbar's first row -> read the first message,
	// Down arrow -> Next (Second Subject) without returning to the
	// list, Up arrow -> Prev (First Subject) again, Q -> back to the
	// message list, Q -> back to the area lightbar, Q -> back to main,
	// Q to log off.
	conn := newFakeConn("M\r\n\r\n\r\n\x1b[B\x1b[AQQQQ\r\n")
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
	if !strings.Contains(renders[0], "First Subject") {
		t.Fatalf("expected first message shown initially, got: %q", renders[0])
	}
	if !strings.Contains(renders[1], "Second Subject") {
		t.Fatalf("expected Down arrow to advance to second message, got: %q", renders[1])
	}
	if !strings.Contains(renders[2], "First Subject") {
		t.Fatalf("expected Up arrow to return to first message, got: %q", renders[2])
	}
}

func TestPostRejectedBelowWriteThreshold(t *testing.T) {
	s := testServer(t)
	// A write-gated area (min_sl_write 100): a regular new user (SL
	// 10) can read it but must be rejected when trying to post.
	if _, err := s.Messages.CreateArea("locked", "Locked Area", "", 0, 100); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}

	// Register a throwaway first account so it (not "bob") absorbs the
	// first-user-becomes-sysop promotion, leaving bob at the requested
	// SLNewUser level.
	if _, err := s.Users.Register("bootstrap-sysop", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register bootstrap sysop: %v", err)
	}
	u, err := s.Users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Areas are listed alphabetically: "General Discussion" (seeded)
	// sorts before "Locked Area", so one Down arrow highlights it in
	// the lightbar before Enter opens it (still empty, so browseArea
	// shows its empty-list P/Q prompt). P is a bare keystroke that
	// triggers the rejection message and its "Press Enter to
	// continue..." pause (a real ReadLine, hence its own CRLF); then Q
	// out of the area's message list, Q out of the area lightbar, Q to
	// log off from main.
	conn := newFakeConn("M\r\n\x1b[B\r\nP\r\nQQQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff; output so far:\n%s", err, conn.out.String())
	}
	if !strings.Contains(conn.out.String(), "don't have permission to post") {
		t.Fatalf("expected permission rejection, got: %q", conn.out.String())
	}
}

func TestSysopCreateMessageArea(t *testing.T) {
	s := testServer(t)
	sysop, err := s.Users.Register("root", "password123", user.SLSysop)
	if err != nil {
		t.Fatalf("Register sysop: %v", err)
	}

	// S -> sysop menu, C -> create area, then fields, M -> back, Q -> quit.
	input := "S\r\nC\r\ndev\r\nDev Talk\r\nFor devs\r\n0\r\n0\r\nM\r\nQ\r\n"
	conn := newFakeConn(input)
	term := NewTerminal(conn)

	err = s.runMenu(term, sysop, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), `Area "Dev Talk" created.`) {
		t.Fatalf("expected creation confirmation, got: %q", conn.out.String())
	}

	area, err := s.Messages.AreaByTag("dev")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if area.Name != "Dev Talk" {
		t.Fatalf("area.Name = %q, want %q", area.Name, "Dev Talk")
	}
}
