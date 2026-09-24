package bbs

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestComposeNetmailToLocalUserDeliversToInbox(t *testing.T) {
	s := testServer(t)
	alice, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	if _, err := s.Users.Register("bob", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register bob: %v", err)
	}

	// N -> netmail inbox (empty), C -> compose, "bob" as recipient,
	// subject, one body line, /S to save, Q out of the (now populated)
	// inbox lightbar, Q to log off from main.
	input := "N\r\nCbob\r\nHello Bob\r\nhi there\r\n/S\r\nQ\r\nQ\r\n"
	conn := newFakeConn(input)
	term := NewTerminal(conn)

	err = s.runMenu(term, alice, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), "Netmail sent.") {
		t.Fatalf("expected send confirmation, got: %q", conn.out.String())
	}

	bob, err := s.Users.ByUsername("bob")
	if err != nil {
		t.Fatalf("ByUsername bob: %v", err)
	}
	inbox, err := s.Netmail.Inbox(bob.ID)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(inbox) != 1 || inbox[0].Subject != "Hello Bob" || inbox[0].FromName != "alice" {
		t.Fatalf("bob's inbox = %+v, want one message from alice titled Hello Bob", inbox)
	}
}

func TestComposeNetmailToUnknownRecipientRejected(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// N -> netmail inbox (empty), C -> compose, an unresolvable
	// recipient that also doesn't look like an FTN address -- the
	// attempt must be rejected before ever reaching the subject
	// prompt, landing back at the (still empty) inbox's C/Q prompt.
	conn := newFakeConn("N\r\nCnosuchuser\r\nQ\r\nQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), "doesn't look like an FTN address") {
		t.Fatalf("expected a rejection explaining the bad recipient, got: %q", conn.out.String())
	}
}

func TestComposeNetmailToFTNAddressQueuesMessage(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// A well-formed FTN address with no local match is accepted and
	// stored, prompts for the remote recipient's name and Crash
	// priority (answered "n" here), and is reported as queued rather
	// than sent, since no BinkP mailer exists yet to actually deliver
	// it.
	input := "N\r\nC1:234/99.0\r\nMike Dreier\r\nn\r\nHi remote\r\nbody\r\n/S\r\nQ\r\nQ\r\n"
	conn := newFakeConn(input)
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "Netmail queued for Mike Dreier at 1:234/99.0") {
		t.Fatalf("expected a queued-for-delivery message naming the recipient, got: %q", out)
	}
}

func TestComposeNetmailToFTNAddressWithCrashSetsCrashFlag(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	input := "N\r\nC1:234/99.0\r\nMike Dreier\r\ny\r\nUrgent\r\nbody\r\n/S\r\nQ\r\nQ\r\n"
	conn := newFakeConn(input)
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}

	pending, err := s.Netmail.PendingOutbound()
	if err != nil {
		t.Fatalf("PendingOutbound: %v", err)
	}
	if len(pending) != 1 || !pending[0].Crash {
		t.Fatalf("PendingOutbound = %+v, want one Crash-flagged message", pending)
	}
}

func TestComposeNetmailToFTNAddressWithBlankNameFallsBackToAddress(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Leaving the recipient-name prompt blank keeps the previous
	// behavior of using the address itself as the display name.
	input := "N\r\nC1:234/99.0\r\n\r\nn\r\nHi remote\r\nbody\r\n/S\r\nQ\r\nQ\r\n"
	conn := newFakeConn(input)
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}

	out := conn.out.String()
	if !strings.Contains(out, "Netmail queued for 1:234/99.0 at 1:234/99.0") {
		t.Fatalf("expected recipient name to fall back to the address, got: %q", out)
	}
}

func TestNetmailInboxShowsNewFlagUntilRead(t *testing.T) {
	s := testServer(t)
	alice, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	bob, err := s.Users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}
	if _, err := s.Netmail.Send(alice.ID, "", bob.ID, "bob", "", "Hi", "hi bob", false); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Visit the inbox twice without reading (must stay flagged NEW),
	// then a third time actually opening the message via Enter (marks
	// it read); the next redraw after backing out must not flag it.
	conn := newFakeConn("N\r\nQ\r\nN\r\n\r\nQQQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, bob, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	renders := strings.Split(out, "[Up/Down] Move   [Enter] Read   [C] Compose   [D] Delete   [Q] Back")
	if len(renders) < 4 {
		t.Fatalf("expected at least three inbox redraws, got %d: %q", len(renders)-1, out)
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

// TestNetmailInboxMergesUnresolvedMailForSysopOnly is a regression
// test for a real gap: an Areafix/Filefix robot's reply is addressed
// to whatever name this system used as its own request's From (e.g.
// "Areafix"), which never resolves to a real local account -- it was
// stored (never discarded) but invisible in the BBS itself, since the
// regular Inbox query is filtered to one specific recipient. A
// sysop's own netmail view must show it; an ordinary user's must not.
func TestNetmailInboxMergesUnresolvedMailForSysopOnly(t *testing.T) {
	s := testServer(t)
	root, err := s.Users.Register("root", "password123", user.SLSysop)
	if err != nil {
		t.Fatalf("Register root: %v", err)
	}
	alice, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	if _, err := s.Netmail.Receive("Areafix", "21:3/100", 0, "Areafix", "", "Re: %LIST", "area list here", root.CreatedAt, false); err != nil {
		t.Fatalf("Receive: %v", err)
	}

	sysopConn := newFakeConn("N\r\nQQ\r\n")
	if err := s.runMenu(NewTerminal(sysopConn), root, 1, "main"); !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu (sysop) error = %v, want errLogoff", err)
	}
	if !strings.Contains(sysopConn.out.String(), "Re: %LIST") {
		t.Fatalf("expected the sysop's netmail view to show the unresolved Areafix reply, got: %q", sysopConn.out.String())
	}

	aliceConn := newFakeConn("N\r\nQQ\r\n")
	if err := s.runMenu(NewTerminal(aliceConn), alice, 1, "main"); !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu (alice) error = %v, want errLogoff", err)
	}
	if strings.Contains(aliceConn.out.String(), "Re: %LIST") {
		t.Fatalf("expected an ordinary user's netmail view NOT to show the unresolved Areafix reply, got: %q", aliceConn.out.String())
	}
}

func TestReplyToNetmailSendsToOriginalSender(t *testing.T) {
	s := testServer(t)
	alice, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	bob, err := s.Users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}
	if _, err := s.Netmail.Send(alice.ID, "", bob.ID, "bob", "", "Original", "hi bob", false); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// N -> inbox (1 message), Enter -> read it, R -> reply, one body
	// line, /S to save, then Q (reader), Q (inbox), Q to log off.
	input := "N\r\n\r\nRThanks\r\n/S\r\nQQQ\r\n"
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
	if !strings.Contains(out, "Reply sent.") {
		t.Fatalf("expected a send confirmation, got: %q", out)
	}

	aliceInbox, err := s.Netmail.Inbox(alice.ID)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(aliceInbox) != 1 {
		t.Fatalf("expected the reply in alice's inbox, got %d messages", len(aliceInbox))
	}
	reply := aliceInbox[0]
	if reply.Subject != "Re: Original" {
		t.Fatalf("reply.Subject = %q, want %q", reply.Subject, "Re: Original")
	}
	if !reply.FromUserID.Valid || reply.FromUserID.Int64 != bob.ID {
		t.Fatalf("reply.FromUserID = %v, want bob's id %d", reply.FromUserID, bob.ID)
	}
	if reply.Body != "Thanks" {
		t.Fatalf("reply.Body = %q, want %q", reply.Body, "Thanks")
	}
}

// TestNetmailListScrollsAndKeepsHeaderAndHintVisible locks in bringing
// the netmail inbox up to the same scrolling behavior
// drawMessageList/drawAreaLightbar already had: with more messages
// than fit in the terminal's 24 rows, it used to just dump every row
// in one shot, pushing the header (and the hotkey hint below the
// table) off the top/bottom of the screen instead of scrolling within
// a fixed viewport.
func TestNetmailListScrollsAndKeepsHeaderAndHintVisible(t *testing.T) {
	s := testServer(t)
	alice, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	bob, err := s.Users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}
	for i := 0; i < 40; i++ {
		if _, err := s.Netmail.Send(alice.ID, "", bob.ID, "bob", "", fmt.Sprintf("Subject %d", i), "body", false); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}

	conn := newFakeConn("")
	term := NewTerminal(conn)
	msgs, err := s.Netmail.Inbox(bob.ID)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	// Select the last message -- if the viewport didn't scroll to
	// follow it, the earlier (buggy) full-dump behavior would still
	// show it, but the header/hint would already be well off-screen.
	if err := s.drawNetmailList(term, bob, msgs, len(msgs)-1); err != nil {
		t.Fatalf("drawNetmailList: %v", err)
	}

	out := conn.out.String()
	if !strings.Contains(out, "Netmail") {
		t.Fatalf("header scrolled off screen, want it still present: %q", out)
	}
	if !strings.Contains(out, "[Up/Down] Move") {
		t.Fatalf("footer hint scrolled off screen, want it still present: %q", out)
	}
	lines := strings.Count(out, "\r\n")
	if lines > 25 {
		t.Fatalf("netmail list printed %d lines, want at most ~24 (the terminal's height): %q", lines, out)
	}
}

// TestReadNetmailArrowsScrollBodyInsteadOfSwitchingMessages locks in
// bringing the netmail reader up to the same fixed-header scroll fix
// drawMessageReader already had: a long body that doesn't fit the
// screen used to push the header off the top with no way to bring it
// back. Up/Down must scroll within the current message's body --
// clamped at the top/bottom -- never switching to a different message
// on their own, since only N/P/Left/Right/Enter are supposed to do
// that.
func TestReadNetmailArrowsScrollBodyInsteadOfSwitchingMessages(t *testing.T) {
	s := testServer(t)
	alice, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	bob, err := s.Users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}
	var bodyLines []string
	for i := 1; i <= 40; i++ {
		bodyLines = append(bodyLines, fmt.Sprintf("body line %d", i))
	}
	body := strings.Join(bodyLines, "\n")
	if _, err := s.Netmail.Send(alice.ID, "", bob.ID, "bob", "", "Long Netmail", body, false); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Open the reader on the only message, scroll down twice, then
	// back up twice, then quit out without ever pressing N/P/arrow-
	// switch keys.
	conn := newFakeConn("N\r\n\r\n\x1b[B\x1b[B\x1b[A\x1b[AQQQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, bob, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "Long Netmail") {
		t.Fatalf("expected the reader to stay on the only message throughout, got: %q", out)
	}
	if !strings.Contains(out, "line 1-") {
		t.Fatalf("expected a scroll-position hint since the body doesn't fit one screen, got: %q", out)
	}
}

// TestReadNetmailFooterPaddedToBottomOfScreen locks in bringing the
// netmail reader up to the same footer-anchoring fix
// drawMessageReader already had: a short body used to leave the
// footer trailing right after it instead of staying anchored near the
// bottom of the screen.
func TestReadNetmailFooterPaddedToBottomOfScreen(t *testing.T) {
	s := testServer(t)
	alice, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	bob, err := s.Users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}
	if _, err := s.Netmail.Send(alice.ID, "", bob.ID, "bob", "", "Short", "just one short line", false); err != nil {
		t.Fatalf("Send: %v", err)
	}
	msgs, err := s.Netmail.Inbox(bob.ID)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}

	conn := newFakeConn("")
	term := NewTerminal(conn)
	if _, err := s.drawNetmailReader(term, msgs, 0, 0); err != nil {
		t.Fatalf("drawNetmailReader: %v", err)
	}

	out := conn.out.String()
	total := strings.Count(out, "\r\n")
	if total < 20 {
		t.Fatalf("drawNetmailReader printed only %d lines for a short message, want the footer padded down near the terminal's 24-row height: %q", total, out)
	}
	if !strings.Contains(out, "[N/Right] Next") {
		t.Fatalf("expected the hotkey hint in output, got: %q", out)
	}
}

// TestNetmailListTopAnchoredNotCenteredOnSelected locks in a
// deliberate behavior change from drawMessageList: the inbox is
// already sorted newest-first, so opening it (selected == 0) must
// show the newest mail at the very top of the screen -- centering the
// viewport on the selection (drawMessageList's own approach) would
// instead scroll the newest mail out of view as soon as selected
// moves past the middle of an available-sized window.
func TestNetmailListTopAnchoredNotCenteredOnSelected(t *testing.T) {
	s := testServer(t)
	alice, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	bob, err := s.Users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}
	for i := 0; i < 40; i++ {
		if _, err := s.Netmail.Send(alice.ID, "", bob.ID, "bob", "", fmt.Sprintf("Subject %d", i), "body", false); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}

	conn := newFakeConn("")
	term := NewTerminal(conn)
	msgs, err := s.Netmail.Inbox(bob.ID)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	// The newest message (index 0, per Inbox's own newest-first order)
	// is also the initially selected one -- centering would still try
	// to scroll upward past it (clamped to 0, so this alone wouldn't
	// catch a regression); what actually distinguishes top-anchored
	// from centered is that the newest message's own row -- "Subject
	// 39" -- ends up the FIRST data row printed, immediately after the
	// column header (itself two lines: the header text, then its own
	// "----" divider), not partway down the screen.
	if err := s.drawNetmailList(term, bob, msgs, 0); err != nil {
		t.Fatalf("drawNetmailList: %v", err)
	}

	out := conn.out.String()
	columnsAt := strings.Index(out, "Date")
	newestAt := strings.Index(out, "Subject 39")
	if columnsAt < 0 || newestAt < 0 {
		t.Fatalf("expected both the columns row and the newest message in output: %q", out)
	}
	between := out[columnsAt:newestAt]
	if strings.Count(between, "\r\n") != 2 {
		t.Fatalf("expected the newest message on the row immediately after the columns header (top-anchored), got %d line(s) between them: %q", strings.Count(between, "\r\n"), out)
	}
}

// TestNetmailListArrowKeysClampAtFirstAndLastInsteadOfWrapping is a
// regression test: Up on the topmost (newest) row used to wrap around
// to the oldest one at the bottom (and Down on the last back to the
// first), which is jarring -- the highlight must just stay put at the
// edge instead -- mirrors messages.go's identically motivated fix.
func TestNetmailListArrowKeysClampAtFirstAndLastInsteadOfWrapping(t *testing.T) {
	s := testServer(t)
	alice, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	bob, err := s.Users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}
	for _, subj := range []string{"MsgOne", "MsgTwo", "MsgThree"} {
		if _, err := s.Netmail.Send(alice.ID, "", bob.ID, "bob", "", subj, "body", false); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}
	readerFooter := "[N/Right] Next  [P/Left] Prev  [Up/Dn] Scroll  [R] Reply  [D] Delete  [Q] Back to list"

	// Up on the already-topmost (newest, MsgThree) row must stay there,
	// not wrap around to the oldest message at the bottom of the list.
	conn := newFakeConn("N\r\n" + strings.Repeat("\x1b[A", 3) + "\r\nQQQ\r\n")
	term := NewTerminal(conn)
	if err := s.runMenu(term, bob, 1, "main"); !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	renders := strings.Split(conn.out.String(), readerFooter)
	if len(renders) < 2 {
		t.Fatalf("expected the reader to open, got: %q", conn.out.String())
	}
	if !strings.Contains(renders[0], "MsgThree") {
		t.Fatalf("expected Up at the newest (first) row to stay there instead of wrapping to the oldest, got: %q", renders[0])
	}

	// Down past the oldest (last, MsgOne) row must stay there, not wrap
	// back around to the newest at the top.
	conn2 := newFakeConn("N\r\n" + strings.Repeat("\x1b[B", 5) + "\r\nQQQ\r\n")
	term2 := NewTerminal(conn2)
	if err := s.runMenu(term2, bob, 1, "main"); !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	renders2 := strings.Split(conn2.out.String(), readerFooter)
	if len(renders2) < 2 {
		t.Fatalf("expected the reader to open, got: %q", conn2.out.String())
	}
	if !strings.Contains(renders2[0], "MsgOne") {
		t.Fatalf("expected Down past the oldest (last) row to stay there instead of wrapping to the newest, got: %q", renders2[0])
	}
}

// TestNetmailListDeleteRemovesMessage covers the D hotkey from the
// list: confirming with "y" must delete the message and it must no
// longer appear in the inbox afterward.
func TestNetmailListDeleteRemovesMessage(t *testing.T) {
	s := testServer(t)
	alice, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	bob, err := s.Users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}
	if _, err := s.Netmail.Send(alice.ID, "", bob.ID, "bob", "", "Delete Me", "body", false); err != nil {
		t.Fatalf("Send: %v", err)
	}

	conn := newFakeConn("N\r\nDy\r\nQQ\r\n")
	term := NewTerminal(conn)
	err = s.runMenu(term, bob, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}

	inbox, err := s.Netmail.Inbox(bob.ID)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(inbox) != 0 {
		t.Fatalf("bob's inbox after confirmed delete = %+v, want empty", inbox)
	}
}

// TestNetmailListDeclineDeleteKeepsMessage covers the "n"/blank answer
// path: the message must survive.
func TestNetmailListDeclineDeleteKeepsMessage(t *testing.T) {
	s := testServer(t)
	alice, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	bob, err := s.Users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}
	if _, err := s.Netmail.Send(alice.ID, "", bob.ID, "bob", "", "Keep Me", "body", false); err != nil {
		t.Fatalf("Send: %v", err)
	}

	conn := newFakeConn("N\r\nDn\r\nQQ\r\n")
	term := NewTerminal(conn)
	err = s.runMenu(term, bob, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}

	inbox, err := s.Netmail.Inbox(bob.ID)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(inbox) != 1 {
		t.Fatalf("bob's inbox after declined delete = %+v, want the message still there", inbox)
	}
}

// TestReadNetmailDeleteReturnsToList covers the D hotkey from the
// reader: confirming deletes the message and returns to the (now
// empty) list instead of trying to keep displaying a deleted message.
func TestReadNetmailDeleteReturnsToList(t *testing.T) {
	s := testServer(t)
	alice, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	bob, err := s.Users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}
	if _, err := s.Netmail.Send(alice.ID, "", bob.ID, "bob", "", "Delete Me", "body", false); err != nil {
		t.Fatalf("Send: %v", err)
	}

	conn := newFakeConn("N\r\n\r\nDy\r\nQQ\r\n")
	term := NewTerminal(conn)
	err = s.runMenu(term, bob, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), "no netmail yet") {
		t.Fatalf("expected to land back on the (now empty) list, got: %q", conn.out.String())
	}

	inbox, err := s.Netmail.Inbox(bob.ID)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(inbox) != 0 {
		t.Fatalf("bob's inbox after confirmed delete = %+v, want empty", inbox)
	}
}
