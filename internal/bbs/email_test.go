package bbs

import (
	"errors"
	"strings"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestComposeEmailQueuesForTheGateway(t *testing.T) {
	s := testServer(t)
	alice, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatal(err)
	}
	s.Email = func() config.EmailConfig { return config.EmailConfig{Enabled: true, Domain: "example.ch"} }

	conn := newFakeConn("N\r\nCjoe@other.ch\r\nHello Joe\r\nhi there\r\n/S\r\nQ\r\nQ\r\n")
	if err := s.runMenu(NewTerminal(conn), alice, 1, "main"); !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	for _, want := range []string{"or email address", "sent as alice@example.ch", "Mail to joe@other.ch is on its way"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q: %q", want, out)
		}
	}
	due, err := s.Netmail.PendingEmail(time.Now())
	if err != nil || len(due) != 1 || due[0].Email != "joe@other.ch" || due[0].Subject != "Hello Joe" || !strings.Contains(due[0].Body, "hi there") {
		t.Fatalf("pending email %+v %v", due, err)
	}
}

func TestComposeEmailWithoutGatewayRefused(t *testing.T) {
	s := testServer(t)
	alice, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn("N\r\nCjoe@other.ch\r\nQ\r\nQ\r\n")
	if err := s.runMenu(NewTerminal(conn), alice, 1, "main"); !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), "Email isn't available on this BBS.") {
		t.Fatalf("no refusal: %q", conn.out.String())
	}
	if due, _ := s.Netmail.PendingEmail(time.Now()); len(due) != 0 {
		t.Fatalf("stored anyway: %+v", due)
	}
}

func TestReplyToEmailGoesBackAsEmail(t *testing.T) {
	s := testServer(t)
	alice, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatal(err)
	}
	s.Email = func() config.EmailConfig { return config.EmailConfig{Enabled: true, Domain: "example.ch"} }
	in, err := s.Netmail.ReceiveEmail("Joe", "joe@other.ch", alice.ID, "alice", "Question", "Are you there?", time.Now(), "<q1@other.ch>", "")
	if err != nil {
		t.Fatal(err)
	}
	// N -> inbox, Enter reads it (sender shown with the address), R
	// replies, one line, /S, then back out.
	conn := newFakeConn("N\r\n\rRyes\r\n/S\r\nQQ\r\nQ\r\n")
	if err := s.runMenu(NewTerminal(conn), alice, 1, "main"); !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), "Joe <joe@other.ch>") {
		t.Errorf("sender without the address: %q", conn.out.String())
	}
	due, err := s.Netmail.PendingEmail(time.Now())
	if err != nil || len(due) != 1 {
		t.Fatalf("pending %+v %v", due, err)
	}
	if due[0].Email != "joe@other.ch" || due[0].Subject != "Re: Question" || due[0].InReplyTo != "<q1@other.ch>" {
		t.Errorf("reply %+v (answering %d)", due[0], in.ID)
	}
}
