package netmail

import (
	"path/filepath"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/db"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

func newTestStore(t *testing.T) (*Store, *user.Store) {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return NewStore(sqlDB), user.NewStore(sqlDB)
}

func TestSendToLocalUserDeliversToInboxAndTracksRead(t *testing.T) {
	s, users := newTestStore(t)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	bob, err := users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}

	sent, err := s.Send(alice.ID, "1:234/56.0", bob.ID, "bob", "", "Hello", "hi bob")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !sent.IsLocal() {
		t.Fatalf("expected the message to resolve local (ToUserID set)")
	}
	if sent.IsRead() {
		t.Fatalf("expected a freshly sent message to be unread")
	}

	inbox, err := s.Inbox(bob.ID)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(inbox) != 1 || inbox[0].Subject != "Hello" || inbox[0].FromName != "alice" {
		t.Fatalf("bob's inbox = %+v, want one message from alice titled Hello", inbox)
	}

	unread, err := s.UnreadCount(bob.ID)
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if unread != 1 {
		t.Fatalf("UnreadCount before reading = %d, want 1", unread)
	}

	// Alice's own inbox must not see bob's mail.
	aliceInbox, err := s.Inbox(alice.ID)
	if err != nil {
		t.Fatalf("Inbox for alice: %v", err)
	}
	if len(aliceInbox) != 0 {
		t.Fatalf("alice's inbox = %+v, want empty", aliceInbox)
	}

	if err := s.MarkRead(sent.ID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	unread, err = s.UnreadCount(bob.ID)
	if err != nil {
		t.Fatalf("UnreadCount after reading: %v", err)
	}
	if unread != 0 {
		t.Fatalf("UnreadCount after reading = %d, want 0", unread)
	}

	// Idempotent: marking an already-read message read again is a no-op.
	if err := s.MarkRead(sent.ID); err != nil {
		t.Fatalf("MarkRead again: %v", err)
	}
}

func TestSendToRemoteAddressLeavesToUserIDUnset(t *testing.T) {
	s, users := newTestStore(t)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}

	sent, err := s.Send(alice.ID, "1:234/56.0", 0, "1:234/99.0", "1:234/99.0", "Hi remote", "body")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sent.IsLocal() {
		t.Fatalf("expected a remote-addressed message to have no local ToUserID")
	}
	if sent.ToAddress != "1:234/99.0" {
		t.Fatalf("ToAddress = %q, want the FTN address", sent.ToAddress)
	}
	if sent.FromAddress != "1:234/56.0" {
		t.Fatalf("FromAddress = %q, want the sender's configured FTN address", sent.FromAddress)
	}
}
