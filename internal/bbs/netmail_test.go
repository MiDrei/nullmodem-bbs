package bbs

import (
	"errors"
	"strings"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/user"
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
	// recipient that also doesn't look like a FidoNet address -- the
	// attempt must be rejected before ever reaching the subject
	// prompt, landing back at the (still empty) inbox's C/Q prompt.
	conn := newFakeConn("N\r\nCnosuchuser\r\nQ\r\nQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), "doesn't look like a FidoNet address") {
		t.Fatalf("expected a rejection explaining the bad recipient, got: %q", conn.out.String())
	}
}

func TestComposeNetmailToFTNAddressQueuesMessage(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// A well-formed FidoNet address with no local match is accepted
	// and stored, but reported as queued rather than sent, since no
	// BinkP mailer exists yet to actually deliver it.
	input := "N\r\nC1:234/99.0\r\nHi remote\r\nbody\r\n/S\r\nQ\r\nQ\r\n"
	conn := newFakeConn(input)
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "Netmail queued for 1:234/99.0") {
		t.Fatalf("expected a queued-for-delivery message, got: %q", out)
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
	if _, err := s.Netmail.Send(alice.ID, "", bob.ID, "bob", "", "Hi", "hi bob"); err != nil {
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
	renders := strings.Split(out, "[Up/Down] Move   [Enter] Read   [C] Compose   [Q] Back")
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
	if _, err := s.Netmail.Send(alice.ID, "", bob.ID, "bob", "", "Original", "hi bob"); err != nil {
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
	if reply.FromUserID != bob.ID {
		t.Fatalf("reply.FromUserID = %d, want bob's id %d", reply.FromUserID, bob.ID)
	}
	if reply.Body != "Thanks" {
		t.Fatalf("reply.Body = %q, want %q", reply.Body, "Thanks")
	}
}
