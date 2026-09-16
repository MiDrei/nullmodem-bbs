package bbs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/message"
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

// TestMessageAreasLightbarHasNoBlankLineAboveColumns is a regression
// test: drawAreaLightbar used to print the banner (which itself
// already leaves the cursor on a fresh blank line, either because the
// .ans file ends in its own "\r\n" or, in the plain-title fallback
// used here since testServer sets no ScreensDir, because Println
// added one) and then add another explicit blank-line separator on
// top, leaving a gap above the Total/New/Yours column header that the
// sysop never asked for -- if a gap is wanted, it belongs in the
// .ans template itself, not hardcoded here.
func TestMessageAreasLightbarHasNoBlankLineAboveColumns(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	conn := newFakeConn("Q\r\nQ\r\n")
	term := NewTerminal(conn)
	stats, err := s.Messages.ListAreaStats(u.SecurityLevel, u.ID)
	if err != nil {
		t.Fatalf("ListAreaStats: %v", err)
	}
	if _, err := s.drawAreaLightbar(term, u, stats, 0, 0); err != nil {
		t.Fatalf("drawAreaLightbar: %v", err)
	}

	out := conn.out.String()
	titleAt := strings.Index(out, "Message Areas")
	columnsAt := strings.Index(out, "Area ")
	if titleAt < 0 || columnsAt < 0 {
		t.Fatalf("area lightbar output missing title or columns row: %q", out)
	}
	// Between the end of the title text and the start of the columns
	// row: just the title's own line terminator (one "\r\n") -- a
	// second would mean an unwanted gap is back.
	betweenTitleAndColumns := out[titleAt:columnsAt]
	if n := strings.Count(betweenTitleAndColumns, "\r\n"); n != 1 {
		t.Fatalf("area lightbar has %d line breaks between title and columns row, want exactly 1 (no gap): %q", n, out)
	}
}

// TestMessageAreasLightbarHasNoBlankLineWithRealAnsFileEnding is a
// regression test for finishHeaderLine: a real .ans banner file (as
// opposed to the plain-title fallback the other no-gap test exercises,
// which testServer falls back to since it sets no ScreensDir) ends
// its last visible row with "\r\n" and THEN an invisible SGR reset
// code with nothing after it -- finishHeaderLine's first
// implementation only checked the string's literal last bytes for a
// trailing "\r\n", which are always the reset code, never "\r\n", so
// it always (wrongly) appended a second terminator on top of the
// file's own.
func TestMessageAreasLightbarHasNoBlankLineWithRealAnsFileEnding(t *testing.T) {
	s := testServer(t)
	dir := t.TempDir()
	s.ScreensDir = dir
	// Mirrors a real hand-designed banner's shape: last visible row,
	// its own "\r\n", then a trailing SGR reset with no newline after
	// it -- exactly what every screen in configs/screens/ looks like.
	if err := os.WriteFile(filepath.Join(dir, "msgareas.ans"), []byte("\x1b[1;36mMessage Areas\r\n\x1b[0m"), 0o644); err != nil {
		t.Fatalf("write msgareas.ans: %v", err)
	}
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	conn := newFakeConn("Q\r\nQ\r\n")
	term := NewTerminal(conn)
	stats, err := s.Messages.ListAreaStats(u.SecurityLevel, u.ID)
	if err != nil {
		t.Fatalf("ListAreaStats: %v", err)
	}
	if _, err := s.drawAreaLightbar(term, u, stats, 0, 0); err != nil {
		t.Fatalf("drawAreaLightbar: %v", err)
	}

	out := conn.out.String()
	titleAt := strings.Index(out, "Message Areas")
	columnsAt := strings.Index(out, "Area ")
	if titleAt < 0 || columnsAt < 0 {
		t.Fatalf("area lightbar output missing title or columns row: %q", out)
	}
	betweenTitleAndColumns := out[titleAt:columnsAt]
	if n := strings.Count(betweenTitleAndColumns, "\r\n"); n != 1 {
		t.Fatalf("area lightbar has %d line breaks between title and columns row for a real .ans-style file, want exactly 1 (no gap): %q", n, out)
	}
}

// TestMessageAreasLightbarScrollsAndKeepsHeaderAndHintVisible is a
// regression test: with more areas than fit in the terminal's 24
// rows, drawAreaLightbar used to just dump every row in one shot,
// pushing the header (and the [Up/Down]/[Enter]/[Q] hint below the
// table) off the top/bottom of the screen instead of scrolling within
// a fixed viewport the way drawMessageList already does.
func TestMessageAreasLightbarScrollsAndKeepsHeaderAndHintVisible(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	for i := 0; i < 40; i++ {
		if _, err := s.Messages.CreateArea(fmt.Sprintf("area%02d", i), fmt.Sprintf("Area %02d", i), "", "", 0, 0); err != nil {
			t.Fatalf("CreateArea: %v", err)
		}
	}

	conn := newFakeConn("")
	term := NewTerminal(conn)
	stats, err := s.Messages.ListAreaStats(u.SecurityLevel, u.ID)
	if err != nil {
		t.Fatalf("ListAreaStats: %v", err)
	}
	// Select the last area -- if the viewport didn't scroll to follow
	// it, the earlier (buggy) full-dump behavior would still show it,
	// but the header/hint would already be well off-screen by then.
	if _, err := s.drawAreaLightbar(term, u, stats, len(stats)-1, 0); err != nil {
		t.Fatalf("drawAreaLightbar: %v", err)
	}

	out := conn.out.String()
	if !strings.Contains(out, "Message Areas") {
		t.Fatalf("header scrolled off screen, want it still present: %q", out)
	}
	if !strings.Contains(out, "[Up/Down] Move") {
		t.Fatalf("footer hint scrolled off screen, want it still present: %q", out)
	}
	lines := strings.Count(out, "\r\n")
	if lines > 25 {
		t.Fatalf("area lightbar printed %d lines, want at most ~24 (the terminal's height): %q", lines, out)
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

// TestMessageAreasLightbarArrowKeysClampAtFirstAndLastInsteadOfWrapping
// is a regression test: Up on the first area used to wrap around to
// the last one (and Down on the last back to the first) -- the
// highlight must just stay put at the edge instead, mirroring
// drawMessageList's identically motivated fix.
func TestMessageAreasLightbarArrowKeysClampAtFirstAndLastInsteadOfWrapping(t *testing.T) {
	s := testServer(t)
	if _, err := s.Messages.CreateArea("second", "Second Area", "", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Up at the first area ("General Discussion", sorting first) must
	// stay put -- an odd number of presses would land on "Second Area"
	// instead if it were still wrapping around modulo the list length.
	conn := newFakeConn("M\r\n" + strings.Repeat("\x1b[A", 3) + "\r\nQQQ\r\n")
	term := NewTerminal(conn)
	if err := s.runMenu(term, u, 1, "main"); !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), "\x1b[1;36mGeneral Discussion\x1b[0m") {
		t.Fatalf("expected Up at the first area to stay on it, got: %q", conn.out.String())
	}

	// Down past the last area ("Second Area") must stay put -- an even
	// number of presses would land back on "General Discussion" instead
	// if it were still wrapping around modulo the list length.
	conn2 := newFakeConn("M\r\n" + strings.Repeat("\x1b[B", 4) + "\r\nQQQ\r\n")
	term2 := NewTerminal(conn2)
	if err := s.runMenu(term2, u, 1, "main"); !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn2.out.String(), "\x1b[1;36mSecond Area\x1b[0m") {
		t.Fatalf("expected Down past the last area to stay on it, got: %q", conn2.out.String())
	}
}

// TestMessageAreasLightbarCursorFollowsToEdgeBeforeWindowScrolls is a
// regression test: drawAreaLightbar used to recompute scrollOffset
// centered on selectedRow every redraw, which mostly pinned the
// highlight to a fixed screen row instead of letting it move within
// the window -- see drawMessageList's identically motivated fix,
// which this mirrors.
func TestMessageAreasLightbarCursorFollowsToEdgeBeforeWindowScrolls(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	for i := 0; i < 40; i++ {
		if _, err := s.Messages.CreateArea(fmt.Sprintf("area%02d", i), fmt.Sprintf("Area %02d", i), "", "", 0, 0); err != nil {
			t.Fatalf("CreateArea: %v", err)
		}
	}
	stats, err := s.Messages.ListAreaStats(u.SecurityLevel, u.ID)
	if err != nil {
		t.Fatalf("ListAreaStats: %v", err)
	}

	conn := newFakeConn("")
	term := NewTerminal(conn)
	scrollOffset, err := s.drawAreaLightbar(term, u, stats, 0, 0)
	if err != nil {
		t.Fatalf("drawAreaLightbar: %v", err)
	}
	if scrollOffset != 0 {
		t.Fatalf("initial scrollOffset = %d, want 0", scrollOffset)
	}
	firstOut := conn.out.String()

	lastVisible := -1
	for i := 0; i < 40; i++ {
		if strings.Contains(firstOut, fmt.Sprintf("Area %02d ", i)) {
			lastVisible = i
		}
	}
	if lastVisible <= 0 || lastVisible >= 39 {
		t.Fatalf("expected the initial window to show only part of the list, last visible = %d, out: %q", lastVisible, firstOut)
	}

	// Highlighting the last row still inside the current window must
	// not scroll it at all -- the highlight moves, the window doesn't.
	conn2 := newFakeConn("")
	term2 := NewTerminal(conn2)
	stillOffset, err := s.drawAreaLightbar(term2, u, stats, lastVisible, scrollOffset)
	if err != nil {
		t.Fatalf("drawAreaLightbar: %v", err)
	}
	if stillOffset != 0 {
		t.Fatalf("scrollOffset moved to %d after highlighting the still-visible last row, want unchanged 0", stillOffset)
	}
	if !strings.Contains(conn2.out.String(), "Area 00 ") {
		t.Fatalf("expected Area 00 still visible (window unmoved), got: %q", conn2.out.String())
	}

	// Moving one row past that edge must scroll the window by exactly
	// one row, keeping the highlight pinned at the bottom edge instead
	// of jumping further or leaving the window fixed.
	conn3 := newFakeConn("")
	term3 := NewTerminal(conn3)
	edgeOffset, err := s.drawAreaLightbar(term3, u, stats, lastVisible+1, stillOffset)
	if err != nil {
		t.Fatalf("drawAreaLightbar: %v", err)
	}
	if edgeOffset != 1 {
		t.Fatalf("scrollOffset after moving one row past the visible edge = %d, want 1 (window follows by exactly one row)", edgeOffset)
	}
	out3 := conn3.out.String()
	if strings.Contains(out3, "Area 00 ") {
		t.Fatalf("expected Area 00 to scroll out of view once the highlight passed the bottom edge, got: %q", out3)
	}
	if !strings.Contains(out3, "Area 01 ") {
		t.Fatalf("expected the window to have scrolled by exactly one row (Area 01 now at top), got: %q", out3)
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

// TestMessageListScrollsAndKeepsHeaderVisibleWithManyMessages locks in
// a real production fix: a message list taller than the terminal used
// to just dump every row in one shot, pushing the header off the top
// of the screen -- the same class of bug drawMessageReader's own
// fixed-header scrolling already addressed for a long message body.
func TestMessageListScrollsAndKeepsHeaderVisibleWithManyMessages(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	general, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	for i := 1; i <= 40; i++ {
		if _, err := s.Messages.PostMessage(general.ID, u.ID, "All", fmt.Sprintf("Subject %d", i), "body"); err != nil {
			t.Fatalf("PostMessage %d: %v", i, err)
		}
	}

	conn := newFakeConn("M\r\n\r\nQQQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	renders := strings.Split(out, "[Up/Down] Move   [Enter] Read   [P] Post   [Q] Back")
	if len(renders) < 2 {
		t.Fatalf("expected at least one message-list redraw, got: %q", out)
	}
	firstRender := renders[0]
	if !strings.Contains(firstRender, "General Discussion") {
		t.Fatalf("expected the header (area name) to stay visible with 40 messages, got: %q", firstRender)
	}
	if !strings.Contains(firstRender, "Subject 1 ") {
		t.Fatalf("expected the first message to appear in the initial view, got: %q", firstRender)
	}
	if strings.Contains(firstRender, "Subject 40") {
		t.Fatalf("expected the last message NOT to be visible in the initial (unscrolled) view, got: %q", firstRender)
	}
	if !strings.Contains(firstRender, "-- 1-") {
		t.Fatalf("expected a scroll-position indicator since the list doesn't fit one screen, got: %q", firstRender)
	}
}

func TestFirstUnreadIndexReturnsFirstUnread(t *testing.T) {
	msgs := []message.Message{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}}
	readIDs := map[int64]bool{1: true, 2: true}
	if got := firstUnreadIndex(msgs, readIDs); got != 2 {
		t.Fatalf("firstUnreadIndex = %d, want 2 (message ID 3, the first unread)", got)
	}
}

func TestFirstUnreadIndexFallsBackToLastWhenAllRead(t *testing.T) {
	msgs := []message.Message{{ID: 1}, {ID: 2}, {ID: 3}}
	readIDs := map[int64]bool{1: true, 2: true, 3: true}
	if got := firstUnreadIndex(msgs, readIDs); got != 2 {
		t.Fatalf("firstUnreadIndex = %d, want 2 (the last message, everything already read)", got)
	}
}

// TestMessageListWindowStartsAtFirstUnreadWhenEnoughNewerMessages
// locks in a real behavior change: entering an area lands on the
// first unread message with the window starting exactly there, not
// showing older already-read messages before it, as long as there
// are enough newer (unread) ones to fill the screen on their own.
func TestMessageListWindowStartsAtFirstUnreadWhenEnoughNewerMessages(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	general, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	var ids []int64
	for i := 1; i <= 40; i++ {
		m, err := s.Messages.PostMessage(general.ID, u.ID, "All", fmt.Sprintf("Subject %d", i), "body")
		if err != nil {
			t.Fatalf("PostMessage %d: %v", i, err)
		}
		ids = append(ids, m.ID)
	}
	for _, id := range ids[:20] {
		if err := s.Messages.MarkMessageRead(u.ID, id); err != nil {
			t.Fatalf("MarkMessageRead: %v", err)
		}
	}

	msgs, err := s.Messages.ListMessages(general.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	readIDs, err := s.Messages.ReadMessageIDs(u.ID, general.ID)
	if err != nil {
		t.Fatalf("ReadMessageIDs: %v", err)
	}
	selected := firstUnreadIndex(msgs, readIDs)
	if selected != 20 {
		t.Fatalf("firstUnreadIndex = %d, want 20 (Subject 21, the first unread)", selected)
	}

	conn := newFakeConn("")
	term := NewTerminal(conn)
	if _, err := s.drawMessageList(term, u, general, msgs, selected, selected, true, readIDs); err != nil {
		t.Fatalf("drawMessageList: %v", err)
	}

	out := conn.out.String()
	if !strings.Contains(out, "Subject 21") {
		t.Fatalf("expected the window to start at the first unread message (Subject 21), got: %q", out)
	}
	if strings.Contains(out, "Subject 20 ") {
		t.Fatalf("expected older, already-read messages before the first unread NOT to be shown when there are enough newer ones to fill the screen, got: %q", out)
	}
}

// TestMessageListWindowPullsBackToFillScreenNearEndOfList locks in the
// other half of the same behavior: when there aren't enough messages
// after the first unread one to fill the screen on their own, the
// window pulls backward to include older, already-read messages
// instead of leaving the rest of the screen blank.
func TestMessageListWindowPullsBackToFillScreenNearEndOfList(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	general, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	var ids []int64
	for i := 1; i <= 40; i++ {
		m, err := s.Messages.PostMessage(general.ID, u.ID, "All", fmt.Sprintf("Subject %d", i), "body")
		if err != nil {
			t.Fatalf("PostMessage %d: %v", i, err)
		}
		ids = append(ids, m.ID)
	}
	// Only the last two messages (39, 40) are unread.
	for _, id := range ids[:38] {
		if err := s.Messages.MarkMessageRead(u.ID, id); err != nil {
			t.Fatalf("MarkMessageRead: %v", err)
		}
	}

	msgs, err := s.Messages.ListMessages(general.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	readIDs, err := s.Messages.ReadMessageIDs(u.ID, general.ID)
	if err != nil {
		t.Fatalf("ReadMessageIDs: %v", err)
	}
	selected := firstUnreadIndex(msgs, readIDs)
	if selected != 38 {
		t.Fatalf("firstUnreadIndex = %d, want 38 (Subject 39, the first unread)", selected)
	}

	conn := newFakeConn("")
	term := NewTerminal(conn)
	if _, err := s.drawMessageList(term, u, general, msgs, selected, selected, true, readIDs); err != nil {
		t.Fatalf("drawMessageList: %v", err)
	}

	out := conn.out.String()
	if !strings.Contains(out, "Subject 39") || !strings.Contains(out, "Subject 40") {
		t.Fatalf("expected both unread messages visible, got: %q", out)
	}
	if !strings.Contains(out, "Subject 30") {
		t.Fatalf("expected the window to pull back and include older, already-read messages to fill the screen instead of leaving it blank, got: %q", out)
	}
	if strings.Contains(out, "Subject 1 ") {
		t.Fatalf("expected the window NOT to pull back all the way to the very first message, got: %q", out)
	}
}

// TestMessageListFooterAnchoredRegardlessOfMessageCount locks in a
// real production fix: the footer hint used to trail right after the
// last message row, so it landed on a different line depending on how
// many messages happened to be in the area. It must always land on
// the same line -- short lists are padded with blank rows so the
// footer stays anchored at a consistent position.
func TestMessageListFooterAnchoredRegardlessOfMessageCount(t *testing.T) {
	renderWithNMessages := func(t *testing.T, n int) string {
		t.Helper()
		s := testServer(t)
		u, err := s.Users.Register("alice", "password123", user.SLNewUser)
		if err != nil {
			t.Fatalf("Register: %v", err)
		}
		general, err := s.Messages.AreaByTag("general")
		if err != nil {
			t.Fatalf("AreaByTag: %v", err)
		}
		for i := 1; i <= n; i++ {
			if _, err := s.Messages.PostMessage(general.ID, u.ID, "All", fmt.Sprintf("Subject %d", i), "body"); err != nil {
				t.Fatalf("PostMessage %d: %v", i, err)
			}
		}

		conn := newFakeConn("M\r\n\r\nQQQ\r\n")
		term := NewTerminal(conn)
		if err := s.runMenu(term, u, 1, "main"); !errors.Is(err, errLogoff) {
			t.Fatalf("runMenu error = %v, want errLogoff", err)
		}
		out := conn.out.String()
		colIdx := strings.Index(out, "Subject")
		hintIdx := strings.Index(out, "[Up/Down] Move   [Enter] Read   [P] Post   [Q] Back")
		if colIdx < 0 || hintIdx < 0 || hintIdx < colIdx {
			t.Fatalf("expected both the column header and the hint line to appear in order, got: %q", out)
		}
		return out[colIdx:hintIdx]
	}

	between1 := renderWithNMessages(t, 1)
	between3 := renderWithNMessages(t, 3)

	lines1 := strings.Count(between1, "\r\n")
	lines3 := strings.Count(between3, "\r\n")
	if lines1 != lines3 {
		t.Fatalf("lines between column header and footer hint = %d (1 message) vs %d (3 messages), want equal -- the footer should be anchored, not trailing right after the last row", lines1, lines3)
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
	readerRenders := strings.Split(out, "[N/Right] Next  [P/Left] Prev  [Up/Dn] Scroll  [R] Reply  [Q] Back to list")
	if len(readerRenders) < 2 {
		t.Fatalf("expected the reader to open, got: %q", out)
	}
	if !strings.Contains(readerRenders[0], "Second Subject") {
		t.Fatalf("expected Down arrow to open the second message, got: %q", readerRenders[0])
	}
}

// TestMessageListArrowKeysClampAtFirstAndLastInsteadOfWrapping is a
// regression test: Up on the first message used to wrap around to the
// last one (and Down on the last back to the first), which is jarring
// -- the highlight must just stay put at the edge instead.
func TestMessageListArrowKeysClampAtFirstAndLastInsteadOfWrapping(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	for _, subj := range []string{"First", "Second", "Third"} {
		if _, err := s.Messages.PostMessage(area.ID, u.ID, "All", subj, "body"); err != nil {
			t.Fatalf("PostMessage: %v", err)
		}
	}
	readerFooter := "[N/Right] Next  [P/Left] Prev  [Up/Dn] Scroll  [R] Reply  [Q] Back to list"

	// Up at the very first message must stay put, not wrap to the last.
	conn := newFakeConn("M\r\n\r\n" + strings.Repeat("\x1b[A", 3) + "\r\nQQQQ\r\n")
	term := NewTerminal(conn)
	if err := s.runMenu(term, u, 1, "main"); !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	renders := strings.Split(conn.out.String(), readerFooter)
	if len(renders) < 2 {
		t.Fatalf("expected the reader to open, got: %q", conn.out.String())
	}
	if !strings.Contains(renders[0], "First") {
		t.Fatalf("expected Up at the first message to stay on it (not wrap to the last), got: %q", renders[0])
	}

	// Down past the last message must stay put, not wrap to the first.
	conn2 := newFakeConn("M\r\n\r\n" + strings.Repeat("\x1b[B", 5) + "\r\nQQQQ\r\n")
	term2 := NewTerminal(conn2)
	if err := s.runMenu(term2, u, 1, "main"); !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	renders2 := strings.Split(conn2.out.String(), readerFooter)
	if len(renders2) < 2 {
		t.Fatalf("expected the reader to open, got: %q", conn2.out.String())
	}
	if !strings.Contains(renders2[0], "Third") {
		t.Fatalf("expected Down past the last message to stay on it (not wrap to the first), got: %q", renders2[0])
	}
}

// TestMessageListCursorFollowsToEdgeBeforeWindowScrolls is a
// regression test: drawMessageList used to recompute scrollOffset as
// exactly selected on every redraw, which pinned the highlight to the
// window's very first row for the whole list and only let it move
// near the end -- visually, the cursor looked stuck mid-screen while
// the list scrolled under it. The window must only start moving once
// the highlight reaches its bottom edge, the same way a normal pager
// scrolls -- see drawMessageList/browseArea's doc comments.
func TestMessageListCursorFollowsToEdgeBeforeWindowScrolls(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	general, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	for i := 1; i <= 40; i++ {
		if _, err := s.Messages.PostMessage(general.ID, u.ID, "All", fmt.Sprintf("Subject %d", i), "body"); err != nil {
			t.Fatalf("PostMessage %d: %v", i, err)
		}
	}
	msgs, err := s.Messages.ListMessages(general.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	readIDs := map[int64]bool{}

	conn := newFakeConn("")
	term := NewTerminal(conn)
	scrollOffset, err := s.drawMessageList(term, u, general, msgs, 0, 0, true, readIDs)
	if err != nil {
		t.Fatalf("drawMessageList: %v", err)
	}
	if scrollOffset != 0 {
		t.Fatalf("initial scrollOffset = %d, want 0", scrollOffset)
	}
	firstOut := conn.out.String()

	lastVisible := 0
	for i := 1; i <= 40; i++ {
		if strings.Contains(firstOut, fmt.Sprintf("Subject %d ", i)) {
			lastVisible = i
		}
	}
	if lastVisible == 0 || lastVisible >= 40 {
		t.Fatalf("expected the initial window to show only part of the list, last visible = %d, out: %q", lastVisible, firstOut)
	}

	// Highlighting the last row still inside the current window must
	// not scroll it at all -- the highlight moves, the window doesn't.
	conn2 := newFakeConn("")
	term2 := NewTerminal(conn2)
	stillOffset, err := s.drawMessageList(term2, u, general, msgs, lastVisible-1, scrollOffset, true, readIDs)
	if err != nil {
		t.Fatalf("drawMessageList: %v", err)
	}
	if stillOffset != 0 {
		t.Fatalf("scrollOffset moved to %d after highlighting the still-visible last row, want unchanged 0", stillOffset)
	}
	if !strings.Contains(conn2.out.String(), "Subject 1 ") {
		t.Fatalf("expected Subject 1 still visible (window unmoved), got: %q", conn2.out.String())
	}

	// Moving one row past that edge must scroll the window by exactly
	// one row, keeping the highlight pinned at the bottom edge instead
	// of jumping further or leaving the window fixed.
	conn3 := newFakeConn("")
	term3 := NewTerminal(conn3)
	edgeOffset, err := s.drawMessageList(term3, u, general, msgs, lastVisible, stillOffset, true, readIDs)
	if err != nil {
		t.Fatalf("drawMessageList: %v", err)
	}
	if edgeOffset != 1 {
		t.Fatalf("scrollOffset after moving one row past the visible edge = %d, want 1 (window follows by exactly one row)", edgeOffset)
	}
	out3 := conn3.out.String()
	if strings.Contains(out3, "Subject 1 ") {
		t.Fatalf("expected Subject 1 to scroll out of view once the highlight passed the bottom edge, got: %q", out3)
	}
	if !strings.Contains(out3, "Subject 2 ") {
		t.Fatalf("expected the window to have scrolled by exactly one row (Subject 2 now at top), got: %q", out3)
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
	if !reply.FromUserID.Valid || reply.FromUserID.Int64 != bob.ID {
		t.Fatalf("reply.FromUserID = %v, want bob's id %d", reply.FromUserID, bob.ID)
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
	// Right arrow -> Next (Second Subject) without returning to the
	// list, Left arrow -> Prev (First Subject) again, Q -> back to the
	// message list, Q -> back to the area lightbar, Q -> back to main,
	// Q to log off. (Up/Down are body-scroll now, not message
	// switching -- see TestReadMessageArrowsScrollBodyInsteadOfSwitchingMessages.)
	conn := newFakeConn("M\r\n\r\n\r\n\x1b[C\x1b[DQQQQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	renders := strings.Split(out, "[N/Right] Next  [P/Left] Prev  [Up/Dn] Scroll  [R] Reply  [Q] Back to list")
	if len(renders) < 4 {
		t.Fatalf("expected at least 3 reader redraws (initial, next, prev), got %d: %q", len(renders)-1, out)
	}
	if !strings.Contains(renders[0], "First Subject") {
		t.Fatalf("expected first message shown initially, got: %q", renders[0])
	}
	if !strings.Contains(renders[1], "Second Subject") {
		t.Fatalf("expected Right arrow to advance to second message, got: %q", renders[1])
	}
	if !strings.Contains(renders[2], "First Subject") {
		t.Fatalf("expected Left arrow to return to first message, got: %q", renders[2])
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
	conn := newFakeConn("M\r\n\r\n\r\n\x1b[D\x1b[C\x1b[CQQQQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	renders := strings.Split(out, "[N/Right] Next  [P/Left] Prev  [Up/Dn] Scroll  [R] Reply  [Q] Back to list")
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

// TestReadMessageArrowsScrollBodyInsteadOfSwitchingMessages locks in
// the fixed-header scroll fix: a long body that doesn't fit the
// screen used to push the header off the top with no way to bring it
// back (see drawMessageReader). Up/Down must now scroll within the
// current message's body -- clamped at the top/bottom, never
// switching to a different message on their own, since only N/P/
// Left/Right/Enter are supposed to do that (a deliberate choice, not
// an oversight).
func TestReadMessageArrowsScrollBodyInsteadOfSwitchingMessages(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	// Comfortably more lines than a 24-row terminal's reader viewport
	// (header + meta + footer eat a chunk of it too) can show at once.
	var bodyLines []string
	for i := 1; i <= 40; i++ {
		bodyLines = append(bodyLines, fmt.Sprintf("body line %d", i))
	}
	body := strings.Join(bodyLines, "\n")
	if _, err := s.Messages.PostMessage(area.ID, u.ID, "All", "Long Message", body); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	// Open the reader on the only message, scroll down twice, then
	// back up twice, then quit out without ever pressing N/P/arrow-
	// left/arrow-right.
	conn := newFakeConn("M\r\n\r\n\r\n\x1b[B\x1b[B\x1b[A\x1b[AQQQQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()

	if !strings.Contains(out, "body line 1\r\n") {
		t.Fatalf("expected the body's first line to appear in the initial view, got: %q", out)
	}
	if !strings.Contains(out, "line 1-") {
		t.Fatalf("expected a scroll-position hint since the body doesn't fit one screen, got: %q", out)
	}
	if strings.Contains(out, "body line 40") {
		t.Fatalf("expected the last line NOT to be visible yet (only scrolled down twice), got: %q", out)
	}

	renders := strings.Split(out, "[Up/Dn] Scroll  [R] Reply  [Q] Back to list")
	if len(renders) < 6 {
		t.Fatalf("expected at least 5 reader redraws (initial + 2 down + 2 up), got %d: %q", len(renders)-1, out)
	}
	for i, r := range renders[:5] {
		if !strings.Contains(r, "Long Message") {
			t.Fatalf("redraw %d left the reader (or switched messages) unexpectedly, got: %q", i, r)
		}
	}
}

// TestReadMessageResolvesANSICursorPositioningViaGrid locks in a real
// production fix on drawMessageReader's own ANSI branch specifically
// (it can't reuse printBody, since it also needs to paginate the
// plain-text case). Two prior attempts both failed against a real
// fsxNet ad: reflowing it via WrapText corrupted the color codes, and
// even passing its bytes through completely untouched still came out
// scrambled, because real ANSI art positions itself with cursor moves
// that assume a blank screen at row 1 col 1 -- not true once a header
// banner has already been printed above it. The fix resolves the art
// against a virtual canvas first (ansi.ParseGrid, the same mechanism
// the web ANSI designer uses to load a .ans file) and prints the
// result, which no longer depends on where on the real screen it
// starts: "Positioned" here is written after a save/newline/restore,
// so it must land on the very same row "Colorful" was on, not two
// rows down where a naively-interpreted bare newline would put it.
func TestReadMessageResolvesANSICursorPositioningViaGrid(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	body := "\x1b[1;33mColorful\x1b[0m\n\x1b[s\n\x1b[uPositioned"
	if _, err := s.Messages.PostMessage(area.ID, u.ID, "All", "ANSI Ad", body); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	conn := newFakeConn("M\r\n\r\n\r\nQQQQ\r\n")
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

// TestReadMessageKeepsHeaderAndScrollsTallANSIArt locks in the fixed-
// header scroll behavior for pre-formatted content (both real ANSI
// escape codes and plain CP437 block art with none at all): unlike
// the earlier full-screen-takeover design, the header/meta banner
// must stay visible and Up/Down must scroll a tall image the same way
// it already does for plain text, since a resolved grid's rows no
// longer depend on where on the real screen they're printed.
func TestReadMessageKeepsHeaderAndScrollsTallANSIArt(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	// Plain block art (0xDB, a solid block) with no escape codes at
	// all -- must still be treated as pre-formatted (see
	// ansi.HasArtBytes) and paginated by row, not word-wrapped.
	var artRows []string
	for i := 1; i <= 40; i++ {
		artRows = append(artRows, fmt.Sprintf("\xdb\xdb\xdb row %d", i))
	}
	body := strings.Join(artRows, "\n")
	if _, err := s.Messages.PostMessage(area.ID, u.ID, "All", "Tall Art", body); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	conn := newFakeConn("M\r\n\r\n\r\n\x1b[B\x1b[BQQQQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()

	if !strings.Contains(out, "Tall Art") {
		t.Fatalf("expected the header (subject) to stay visible for pre-formatted content, got: %q", out)
	}
	if !strings.Contains(out, "row 1\r\n") {
		t.Fatalf("expected the first art row to appear in the initial view, got: %q", out)
	}
	if !strings.Contains(out, "line 1-") {
		t.Fatalf("expected a scroll-position hint since the art doesn't fit one screen, got: %q", out)
	}
	if strings.Contains(out, "row 40") {
		t.Fatalf("expected the last row NOT to be visible yet (only scrolled down twice), got: %q", out)
	}
}

// TestReadMessageScrollStatusStaysOnItsOwnFooterLine locks in a real
// production fix: the scroll-status text ("-- line X-Y of Z --") used
// to be prepended directly onto the hotkey hint on one shared line --
// with large enough line numbers that combined line exceeded the
// terminal's width and wrapped on its own, silently consuming one
// more physical row than drawMessageReader had budgeted for and
// pushing the header off the top of the screen. The scroll status
// must always be on its own dedicated line, never concatenated onto
// the hint.
func TestReadMessageScrollStatusStaysOnItsOwnFooterLine(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	// 200 lines so the rendered "-- line X-Y of 200 --" status is
	// realistically long -- concatenated onto the hotkey hint, this
	// combination is well past 80 columns.
	var bodyLines []string
	for i := 1; i <= 200; i++ {
		bodyLines = append(bodyLines, fmt.Sprintf("line %d", i))
	}
	body := strings.Join(bodyLines, "\n")
	if _, err := s.Messages.PostMessage(area.ID, u.ID, "All", "Very Long Message", body); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	conn := newFakeConn("M\r\n\r\n\r\nQQQQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()

	statusIdx := strings.Index(out, "-- line")
	if statusIdx < 0 {
		t.Fatalf("expected a scroll-status hint, got: %q", out)
	}
	hintOffset := strings.Index(out[statusIdx:], "[N/Right]")
	if hintOffset < 0 {
		t.Fatalf("expected the hotkey hint to appear after the scroll status, got: %q", out)
	}
	between := out[statusIdx : statusIdx+hintOffset]
	if !strings.Contains(between, "\r\n") {
		t.Fatalf("expected the scroll status and the hotkey hint on separate lines, got them joined: %q", between)
	}
}

// TestReadMessageFooterPaddedToBottomOfScreen is a regression test:
// unlike drawMessageList/drawAreaLightbar, drawMessageReader never
// padded a short body out to the viewport's full height, so the
// footer (scroll status + hotkey hint) trailed right after a short
// message instead of staying anchored near the bottom of the screen.
func TestReadMessageFooterPaddedToBottomOfScreen(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if _, err := s.Messages.PostMessage(area.ID, u.ID, "All", "Short", "just one short line"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	msgs, err := s.Messages.ListMessages(area.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}

	conn := newFakeConn("")
	term := NewTerminal(conn)
	if _, err := s.drawMessageReader(term, u, area, msgs, 0, 0); err != nil {
		t.Fatalf("drawMessageReader: %v", err)
	}

	out := conn.out.String()
	total := strings.Count(out, "\r\n")
	// fakeConn reports a 24-row window (Terminal.Height()'s default
	// too); with the footer correctly anchored at the bottom, the
	// whole redraw should occupy close to that many rows even though
	// the body itself is one line -- before padding was added, it
	// fell far short (just header+meta+one body line+footer).
	if total < 20 {
		t.Fatalf("drawMessageReader printed only %d lines for a short message, want the footer padded down near the terminal's 24-row height: %q", total, out)
	}
	if !strings.Contains(out, "[N/Right] Next") {
		t.Fatalf("expected the hotkey hint in output, got: %q", out)
	}
}

// TestReadMessageFooterUsesCustomTemplate locks in msgread-footer.ans
// as a customizable screen file, mirroring every other piece of the
// reader/lightbar UI (msgread.ans, msgread-meta.ans, msgareas-*.ans,
// ...): a sysop who wants a gap, a different layout, or extra
// decoration around the scroll status/hotkey hint can do so in the
// template instead of it being hardcoded.
// TestReadMessageMetaTemplateScreenClearDoesNotWipeHeader is a
// regression test: a custom msgread-meta.ans authored with its own
// leading clear-screen+home sequence (e.g. saved via the web ANSI
// designer, which defaults to one for a standalone screen) used to
// wipe out the header banner drawn just before it and snap the cursor
// back to row 1 -- throwing off the viewport's line-count budget
// along with it (less body fit on screen than should have, and the
// footer landed short of the real bottom) since the budget still
// reserved rows for a header that no longer visually appeared.
func TestReadMessageMetaTemplateScreenClearDoesNotWipeHeader(t *testing.T) {
	s := testServer(t)
	dir := t.TempDir()
	s.ScreensDir = dir
	if err := os.WriteFile(filepath.Join(dir, "msgread.ans"), []byte("\x1b[2J\x1b[HHEADER BANNER"), 0o644); err != nil {
		t.Fatalf("write msgread.ans: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "msgread-meta.ans"), []byte("\x1b[2J\x1b[HFrom: {FROM:-40} Date: {DATE}"), 0o644); err != nil {
		t.Fatalf("write msgread-meta.ans: %v", err)
	}
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if _, err := s.Messages.PostMessage(area.ID, u.ID, "All", "Hi", "hello"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	msgs, err := s.Messages.ListMessages(area.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}

	conn := newFakeConn("")
	term := NewTerminal(conn)
	if _, err := s.drawMessageReader(term, u, area, msgs, 0, 0); err != nil {
		t.Fatalf("drawMessageReader: %v", err)
	}

	out := conn.out.String()
	if !strings.Contains(out, "HEADER BANNER") {
		t.Fatalf("meta template's own screen-clear wiped out the header banner, want it still present: %q", out)
	}
	headerAt := strings.Index(out, "HEADER BANNER")
	fromAt := strings.Index(out, "From:")
	if headerAt < 0 || fromAt < 0 || headerAt > fromAt {
		t.Fatalf("expected the header banner to appear before the meta block, got: %q", out)
	}
}

func TestReadMessageFooterUsesCustomTemplate(t *testing.T) {
	s := testServer(t)
	dir := t.TempDir()
	s.ScreensDir = dir
	if err := os.WriteFile(filepath.Join(dir, "msgread-footer.ans"), []byte("CUSTOM FOOTER {HINT} status={SCROLLSTATUS}"), 0o644); err != nil {
		t.Fatalf("write msgread-footer.ans: %v", err)
	}
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if _, err := s.Messages.PostMessage(area.ID, u.ID, "All", "Hi", "hello"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	msgs, err := s.Messages.ListMessages(area.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}

	conn := newFakeConn("")
	term := NewTerminal(conn)
	if _, err := s.drawMessageReader(term, u, area, msgs, 0, 0); err != nil {
		t.Fatalf("drawMessageReader: %v", err)
	}

	out := conn.out.String()
	if !strings.Contains(out, "CUSTOM FOOTER") {
		t.Fatalf("expected the custom msgread-footer.ans template to be used, got: %q", out)
	}
	if !strings.Contains(out, "[N/Right] Next") {
		t.Fatalf("expected {HINT} to be substituted with the hotkey hint, got: %q", out)
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

// TestPrintBodyResolvesANSICursorPositioningViaGrid locks in a real
// production fix: a message/description body containing real ANSI
// escape sequences (a BBS ad tossed into an ANSI-tagged area, an
// ANSImation, etc.) came out scrambled three times over -- first
// because ansi.WrapText treated every escape-sequence byte as an
// ordinary character to word-wrap; then, even sending its bytes
// through completely untouched still corrupted it, because Print's
// automatic bare-LF-to-CRLF translation inserted a \r real ANSI art
// didn't expect; and even bypassing that, real ANSI art's absolute/
// relative cursor positioning assumes it's drawing on a blank screen
// starting at row 1 col 1, which is false once other content (a
// header banner) has already been printed above it. The fix resolves
// the art against a virtual canvas first (ansi.ParseGrid, the same
// mechanism the web ANSI designer uses to load a .ans file) and
// prints the result, which no longer depends on where on the real
// screen it starts: "Positioned" here is written after a save/
// newline/restore, so it must land on the very same row "Colorful"
// was on, not two rows down where a naively-interpreted bare newline
// would put it.
func TestPrintBodyResolvesANSICursorPositioningViaGrid(t *testing.T) {
	conn := newFakeConn("")
	term := NewTerminal(conn)
	body := "\x1b[1;33mColorful\x1b[0m\n\x1b[s\n\x1b[uPositioned"

	var b strings.Builder
	b.WriteString("header\r\n")
	if err := printBody(term, &b, body, "footer", 80); err != nil {
		t.Fatalf("printBody: %v", err)
	}

	got := conn.out.String()
	colorfulIdx := strings.Index(got, "Colorful")
	positionedIdx := strings.Index(got, "Positioned")
	if colorfulIdx < 0 || positionedIdx < 0 {
		t.Fatalf("expected both %q and %q to appear in the output, got: %q", "Colorful", "Positioned", got)
	}
	between := got[colorfulIdx:positionedIdx]
	if strings.Count(between, "\r\n") != 1 {
		t.Fatalf("expected exactly one line break between Colorful and Positioned (cursor save/restore resolved onto the same row), got %d in %q", strings.Count(between, "\r\n"), between)
	}
	if !strings.Contains(got, "footer") {
		t.Fatalf("expected the footer to still be printed after the resolved art, got: %q", got)
	}
}

// TestPrintBodyWordWrapsPlainText confirms ordinary prose still gets
// the normal word-wrap treatment (printBody isn't a blanket bypass).
func TestPrintBodyWordWrapsPlainText(t *testing.T) {
	conn := newFakeConn("")
	term := NewTerminal(conn)
	body := "this is a perfectly ordinary message with no ANSI codes in it at all"

	var b strings.Builder
	if err := printBody(term, &b, body, "footer", 20); err != nil {
		t.Fatalf("printBody: %v", err)
	}

	got := conn.out.String()
	if strings.Count(got, "\r\n") < 2 {
		t.Fatalf("printBody output = %q, want it word-wrapped across multiple lines at width 20", got)
	}
}

func TestStripSeenByAndPathForDisplayRemovesTrailingRoutingLinesButKeepsFooter(t *testing.T) {
	body := "the actual message text\n" +
		"\n" +
		"--- Mystic BBS v1.12 A49 (Linux/64)\n" +
		"* Origin: TheForze - bbs.theforze.eu:23 (21:3/126)\n" +
		"SEEN-BY: 1/100 179 2/100 116 3/100 105\n" +
		"SEEN-BY: 3/141 156 158\n" +
		"PATH: 3/126 100\n"
	got := stripSeenByAndPathForDisplay(body)
	want := "the actual message text\n" +
		"\n" +
		"--- Mystic BBS v1.12 A49 (Linux/64)\n" +
		"* Origin: TheForze - bbs.theforze.eu:23 (21:3/126)"
	if got != want {
		t.Fatalf("stripSeenByAndPathForDisplay() = %q, want %q", got, want)
	}
}

func TestStripSeenByAndPathForDisplayLeavesBodyUnchangedWhenNeitherPresent(t *testing.T) {
	body := "just an ordinary message\nwith no routing footer"
	if got := stripSeenByAndPathForDisplay(body); got != body {
		t.Fatalf("stripSeenByAndPathForDisplay() = %q, want body unchanged (%q)", got, body)
	}
}

// TestStripSeenByAndPathForDisplayHandlesKludgedPath locks in a real
// production fix: PATH commonly arrives \x01-kludged even though
// SEEN-BY doesn't (observed live) -- since the scan works backward
// from the end and stops at the first non-matching line, an
// unstripped leading \x01 on PATH alone left the entire block,
// SEEN-BY lines included, still showing.
func TestStripSeenByAndPathForDisplayHandlesKludgedPath(t *testing.T) {
	body := "the actual message text\n" +
		"SEEN-BY: 1/100 179 2/100 116\n" +
		"\x01PATH: 3/126 100\n"
	got := stripSeenByAndPathForDisplay(body)
	want := "the actual message text"
	if got != want {
		t.Fatalf("stripSeenByAndPathForDisplay() = %q, want %q", got, want)
	}
}
