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
	if _, err := s.Messages.CreateArea("second", "Second Area", "", "", 0, 0); err != nil {
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

func TestMessageAreasLightbarNewCountUnaffectedByJustVisitingList(t *testing.T) {
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

	// Enter the area and immediately leave again WITHOUT opening the
	// message -- merely visiting the message list must not clear the
	// area lightbar's New count; only actually reading a message does
	// (see TestMessageAreasLightbarNewCountClearsAfterReadingMessage).
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
	if !strings.Contains(renders[1], "     1      1      1") {
		t.Fatalf("expected New still 1 after just visiting the list without reading, got: %q", renders[1])
	}
}

func TestMessageAreasLightbarNewCountClearsAfterReadingMessage(t *testing.T) {
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

	// Enter the area, open the message in the reader (Enter on the
	// message list's only, already-highlighted row), leave, and check
	// the area lightbar's second draw shows New=0.
	conn := newFakeConn("M\r\n\r\n\r\nQQQQ\r\n")
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
		t.Fatalf("expected New=0 after reading the message, got: %q", renders[1])
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

func TestMessageAreasLightbarGroupsByNetwork(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := s.Messages.CreateArea("fido", "Fido Chat", "", "FidoNet", 0, 0); err != nil {
		t.Fatalf("CreateArea fido: %v", err)
	}
	if _, err := s.Messages.CreateArea("fsx", "Fsx Chat", "", "fsxNet", 0, 0); err != nil {
		t.Fatalf("CreateArea fsx: %v", err)
	}

	conn := newFakeConn("M\r\nQ\r\nQ\r\n")
	term := NewTerminal(conn)
	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()

	// Areas sort network ("" first, byte order thereafter), sort_order,
	// name: the seeded local "General Discussion" (no network), then
	// "FidoNet"'s divider and area, then "fsxNet"'s divider and area.
	generalIdx := strings.Index(out, "General Discussion")
	fidoDividerIdx := strings.Index(out, "FidoNet")
	fidoAreaIdx := strings.Index(out, "Fido Chat")
	fsxDividerIdx := strings.Index(out, "fsxNet")
	fsxAreaIdx := strings.Index(out, "Fsx Chat")
	if generalIdx < 0 || fidoDividerIdx < 0 || fidoAreaIdx < 0 || fsxDividerIdx < 0 || fsxAreaIdx < 0 {
		t.Fatalf("expected local area, both network dividers, and both network areas present, got: %q", out)
	}
	if !(generalIdx < fidoDividerIdx && fidoDividerIdx < fidoAreaIdx && fidoAreaIdx < fsxDividerIdx && fsxDividerIdx < fsxAreaIdx) {
		t.Fatalf("expected order General Discussion < FidoNet divider < Fido Chat < fsxNet divider < Fsx Chat, got: %q", out)
	}
	// The local area has no network, so no divider immediately
	// precedes it -- only one divider each for FidoNet/fsxNet.
	if strings.Count(out, "FidoNet") != 1 {
		t.Fatalf(`expected exactly one "FidoNet" divider, got %d: %q`, strings.Count(out, "FidoNet"), out)
	}
	if strings.Count(out, "fsxNet") != 1 {
		t.Fatalf(`expected exactly one "fsxNet" divider, got %d: %q`, strings.Count(out, "fsxNet"), out)
	}
}

func TestMessageListLightbarShowsNewFlagUntilActuallyRead(t *testing.T) {
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

	// Visit the message list twice (Enter the area, Q back out, Enter
	// again) WITHOUT opening the message -- it must stay flagged NEW
	// both times. Only the third visit, where Enter opens the message
	// in the reader, actually marks it read; the list's next redraw
	// (after backing out of the reader) must no longer flag it.
	conn := newFakeConn("M\r\n\r\nQ\r\n\r\nQQQQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	renders := strings.Split(out, "[Up/Down] Move   [Enter] Read   [P] Post   [Q] Back")
	if len(renders) < 4 {
		t.Fatalf("expected at least three message-list redraws, got %d: %q", len(renders)-1, out)
	}
	if !strings.Contains(renders[0], "NEW") {
		t.Fatalf("expected the message flagged NEW on the first visit, got: %q", renders[0])
	}
	if !strings.Contains(renders[1], "NEW") {
		t.Fatalf("expected the message still flagged NEW on the second visit (not yet read), got: %q", renders[1])
	}
	if strings.Contains(renders[2], "NEW") {
		t.Fatalf("expected the NEW flag gone after actually reading the message, got: %q", renders[2])
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
	if !strings.Contains(out, "\x1b[47m\x1b[30mNEW Second Subject") {
		t.Fatalf("expected Second Subject's row highlighted, got: %q", out)
	}
	readerRenders := strings.Split(out, "[Enter/Dn/Right] Next  [Up/Left] Prev  [R] Reply  [Q] Back to list")
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
	// subject, two body lines, "/S" to save. browseArea's outer loop
	// refetches and now shows the message-list lightbar with the new
	// post highlighted; Enter opens the reader, Q backs out of the
	// reader, Q out of the message list, Q out of the area lightbar,
	// Q to log off from main.
	input := "M\r\n\r\nPHello World\r\nLine one\r\nLine two\r\n/S\r\n\r\nQQQQ\r\n"
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

func TestPostMessageEditorDeleteLineCommand(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// P -> post, subject, three lines, "/D 2" deletes "Line B", "/L"
	// lists what's left (for coverage), "/S" saves. Then read the
	// posted message back to confirm the deleted line is really gone.
	input := "M\r\n\r\nPDelete Test\r\nLine A\r\nLine B\r\nLine C\r\n/D 2\r\n/L\r\n/S\r\n\r\nQQQQ\r\n"
	conn := newFakeConn(input)
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "Line 2 deleted.") {
		t.Fatalf("expected delete confirmation, got: %q", out)
	}
	if !strings.Contains(out, "Message posted.") {
		t.Fatalf("expected posting confirmation, got: %q", out)
	}
	// The editor echoes every typed line once as it's entered (a real
	// terminal shows you what you type), so "Line B" appears exactly
	// once from that echo -- it must not appear a second time in the
	// /L listing or the read view, which is where a surviving line
	// would show up twice.
	if strings.Count(out, "Line A") < 2 || strings.Count(out, "Line C") < 2 {
		t.Fatalf("expected the surviving lines in both the /L listing and the read view, got: %q", out)
	}
	if strings.Count(out, "Line B") != 1 {
		t.Fatalf("expected the deleted line to appear only once (its typed echo), got %d times: %q", strings.Count(out, "Line B"), out)
	}
}

func TestPostMessageEditorAbortCommand(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}

	// P -> post, subject, one line, "/A" aborts -- nothing should be
	// saved, so the area's message list stays empty.
	input := "M\r\n\r\nPAbort Test\r\nnever mind\r\n/A\r\nQQQ\r\n"
	conn := newFakeConn(input)
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), "Message aborted.") {
		t.Fatalf("expected abort confirmation, got: %q", conn.out.String())
	}
	msgs, err := s.Messages.ListMessages(area.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no messages after abort, got %d", len(msgs))
	}
}

func TestReplyToMessagePrefillsToAndSubjectAndPosts(t *testing.T) {
	s := testServer(t)
	alice, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	bob, err := s.Users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}
	area, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if _, err := s.Messages.PostMessage(area.ID, alice.ID, "All", "Original", "hello"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	// M -> areas lightbar, Enter -> General Discussion, Enter again on
	// the message list's only row -> read the original, R -> reply,
	// one body line, /S to save, then unwind: Q (reader), Q (list,
	// now has 2 messages), Q (area lightbar), Q to log off.
	input := "M\r\n\r\n\r\nRThanks for that\r\n/S\r\nQQQQ\r\n"
	conn := newFakeConn(input)
	term := NewTerminal(conn)

	err = s.runMenu(term, bob, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "Re: Original") {
		t.Fatalf("expected the prefilled \"Re: \" subject, got: %q", out)
	}
	if !strings.Contains(out, "Reply posted.") {
		t.Fatalf("expected a post confirmation, got: %q", out)
	}

	msgs, err := s.Messages.ListMessages(area.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages after the reply, got %d", len(msgs))
	}
	reply := msgs[1]
	if reply.Subject != "Re: Original" {
		t.Fatalf("reply.Subject = %q, want %q", reply.Subject, "Re: Original")
	}
	if reply.ToName != "alice" {
		t.Fatalf("reply.ToName = %q, want %q (the original author)", reply.ToName, "alice")
	}
	if reply.FromUserID != bob.ID {
		t.Fatalf("reply.FromUserID = %d, want bob's id %d", reply.FromUserID, bob.ID)
	}
	if reply.Body != "Thanks for that" {
		t.Fatalf("reply.Body = %q, want %q", reply.Body, "Thanks for that")
	}
}

func TestReplyToMessageRejectedBelowWriteThreshold(t *testing.T) {
	s := testServer(t)
	if _, err := s.Messages.CreateArea("locked", "Locked Area", "", "", 0, 100); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	// A throwaway first account absorbs the first-user-becomes-sysop
	// promotion, leaving bob at the requested SLNewUser level.
	bootstrap, err := s.Users.Register("bootstrap-sysop", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bootstrap: %v", err)
	}
	bob, err := s.Users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}
	area, err := s.Messages.AreaByTag("locked")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if _, err := s.Messages.PostMessage(area.ID, bootstrap.ID, "All", "Original", "hello"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	// Areas are listed alphabetically: "General Discussion" (seeded)
	// sorts before "Locked Area", so one Down arrow highlights it in
	// the lightbar before Enter opens it, then Enter again opens the
	// only message. R attempts a reply bob's SL (10) doesn't allow;
	// the rejection is a paused message (see pauseForKey), so a bare
	// Enter dismisses it before unwinding with Q's.
	input := "M\r\n\x1b[B\r\n\r\nR\r\nQQQQ\r\n"
	conn := newFakeConn(input)
	term := NewTerminal(conn)

	err = s.runMenu(term, bob, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), "don't have permission to post") {
		t.Fatalf("expected a permission rejection, got: %q", conn.out.String())
	}
	msgs, err := s.Messages.ListMessages(area.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected no reply to have been posted, got %d messages", len(msgs))
	}
}

func TestPostMessageEditorRejectsEmptySave(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// P -> post, subject, "/S" with no lines yet -- must be rejected
	// instead of posting an empty message; "/A" then cleanly aborts.
	input := "M\r\n\r\nPEmpty Test\r\n/S\r\n/A\r\nQQQ\r\n"
	conn := newFakeConn(input)
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "Message is empty; nothing to save.") {
		t.Fatalf("expected empty-save rejection, got: %q", out)
	}
	if strings.Contains(out, "Message posted.") {
		t.Fatalf("expected nothing to have been posted, got: %q", out)
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
	renders := strings.Split(out, "[Enter/Dn/Right] Next  [Up/Left] Prev  [R] Reply  [Q] Back to list")
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

func TestReadMessageNextPrevClampAtEnds(t *testing.T) {
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

	// Open the reader on the first (and only highlighted) message,
	// press Prev/Left immediately -- it must stay on the first message
	// instead of wrapping to the last one. Then advance to the last
	// message and press Next/Right again -- it must stay there instead
	// of wrapping back to the first.
	conn := newFakeConn("M\r\n\r\n\r\n\x1b[A\x1b[C\x1b[CQQQQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	renders := strings.Split(out, "[Enter/Dn/Right] Next  [Up/Left] Prev  [R] Reply  [Q] Back to list")
	if len(renders) < 4 {
		t.Fatalf("expected at least 3 reader redraws (initial, after Prev, after Next), got %d: %q", len(renders)-1, out)
	}
	if !strings.Contains(renders[0], "First Subject") {
		t.Fatalf("expected first message shown initially, got: %q", renders[0])
	}
	if !strings.Contains(renders[1], "First Subject") {
		t.Fatalf("expected Prev at the first message to stay put, got: %q", renders[1])
	}
	if !strings.Contains(renders[2], "Second Subject") {
		t.Fatalf("expected Next to advance to the last message, got: %q", renders[2])
	}
	if !strings.Contains(renders[3], "Second Subject") {
		t.Fatalf("expected Next at the last message to stay put instead of wrapping, got: %q", renders[3])
	}
}

func TestPostRejectedBelowWriteThreshold(t *testing.T) {
	s := testServer(t)
	// A write-gated area (min_sl_write 100): a regular new user (SL
	// 10) can read it but must be rejected when trying to post.
	if _, err := s.Messages.CreateArea("locked", "Locked Area", "", "", 0, 100); err != nil {
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

	// S -> sysop menu, C -> create area, then fields (including the
	// new Network prompt), M -> back, Q -> quit.
	input := "S\r\nC\r\ndev\r\nDev Talk\r\nFor devs\r\nfsxNet\r\n0\r\n0\r\nM\r\nQ\r\n"
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
	if area.Network != "fsxNet" {
		t.Fatalf("area.Network = %q, want %q", area.Network, "fsxNet")
	}
}
