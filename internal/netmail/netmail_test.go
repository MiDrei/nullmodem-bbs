package netmail

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/db"
	"github.com/midrei/nullmodem-bbs/internal/user"
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

	sent, err := s.Send(alice.ID, "1:234/56.0", bob.ID, "bob", "", "Hello", "hi bob", false)
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

	sent, err := s.Send(alice.ID, "1:234/56.0", 0, "1:234/99.0", "1:234/99.0", "Hi remote", "body", false)
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

func TestPendingOutboundAndMarkSent(t *testing.T) {
	s, users := newTestStore(t)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}

	// A local message and a remote one that's already been marked
	// sent must not show up in PendingOutbound; only the still-queued
	// remote message should.
	if _, err := s.Send(alice.ID, "1:234/56.0", 0, "notpending", "1:234/1.0", "Already sent", "body", false); err != nil {
		t.Fatalf("Send (already sent): %v", err)
	}
	already, err := s.Send(alice.ID, "1:234/56.0", 0, "notpending", "1:234/1.0", "Already sent", "body", false)
	if err != nil {
		t.Fatalf("Send (to mark sent): %v", err)
	}
	if err := s.MarkSent(already.ID); err != nil {
		t.Fatalf("MarkSent: %v", err)
	}

	pending, err := s.Send(alice.ID, "1:234/56.0", 0, "1:234/99.0", "1:234/99.0", "Still queued", "body", false)
	if err != nil {
		t.Fatalf("Send (pending): %v", err)
	}

	outbound, err := s.PendingOutbound()
	if err != nil {
		t.Fatalf("PendingOutbound: %v", err)
	}
	if len(outbound) != 2 {
		t.Fatalf("PendingOutbound = %+v, want 2 messages (excluding the already-sent one)", outbound)
	}
	for _, m := range outbound {
		if m.ID == already.ID {
			t.Fatalf("PendingOutbound included message %d, which was already marked sent", already.ID)
		}
	}

	if err := s.MarkSent(pending.ID); err != nil {
		t.Fatalf("MarkSent: %v", err)
	}
	outbound, err = s.PendingOutbound()
	if err != nil {
		t.Fatalf("PendingOutbound after marking sent: %v", err)
	}
	for _, m := range outbound {
		if m.ID == pending.ID {
			t.Fatalf("PendingOutbound still included message %d after MarkSent", pending.ID)
		}
	}

	// Idempotent: marking an already-sent message sent again is a no-op.
	if err := s.MarkSent(pending.ID); err != nil {
		t.Fatalf("MarkSent again: %v", err)
	}
}

func TestReceiveStoresRemoteSenderWithoutLocalAccount(t *testing.T) {
	s, users := newTestStore(t)
	bob, err := users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}

	written := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	received, err := s.Receive("Mike Dreier", "21:3/194", bob.ID, "bob", "", "Hello from FidoNet", "hi there", written, false)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if received.IsFromRemote() != true {
		t.Fatalf("expected IsFromRemote() = true for a message with no local sender")
	}
	if received.FromName != "Mike Dreier" {
		t.Fatalf("FromName = %q, want %q", received.FromName, "Mike Dreier")
	}
	if received.FromAddress != "21:3/194" {
		t.Fatalf("FromAddress = %q, want %q", received.FromAddress, "21:3/194")
	}
	if !received.PostedAt.Equal(written) {
		t.Fatalf("PostedAt = %v, want the message's own Written time %v", received.PostedAt, written)
	}

	inbox, err := s.Inbox(bob.ID)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(inbox) != 1 || inbox[0].FromName != "Mike Dreier" {
		t.Fatalf("bob's inbox = %+v, want one message from Mike Dreier", inbox)
	}
}

// TestInboxFromAddressFindsUnresolvedRecipientMessages is a
// regression-shaped test for a real gap: an inbound reply addressed
// to a name that doesn't match any local username (e.g. "Areafix",
// mirroring what we sent as our own request's From name) has no
// to_user_id and so never appears in any user's Inbox -- InboxFromAddress
// must still find it by sender address alone.
func TestInboxFromAddressFindsUnresolvedRecipientMessages(t *testing.T) {
	s, _ := newTestStore(t)

	written := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	if _, err := s.Receive("Areafix", "21:3/100", 0, "Areafix", "", "Re: Areas", "area list here", written, false); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	// A message from a different sender must not show up.
	if _, err := s.Receive("Someone", "21:3/200", 0, "Areafix", "", "unrelated", "body", written, false); err != nil {
		t.Fatalf("Receive: %v", err)
	}

	got, err := s.InboxFromAddress("21:3/100", 10)
	if err != nil {
		t.Fatalf("InboxFromAddress: %v", err)
	}
	if len(got) != 1 || got[0].Subject != "Re: Areas" {
		t.Fatalf("InboxFromAddress(21:3/100) = %+v, want exactly the one message from that address", got)
	}
}

// TestUnresolvedInboxFindsMessagesWithNoMatchingLocalUser is a
// regression-shaped test for a real gap: an Areafix/Filefix robot's
// reply is addressed to whatever name this system used as its own
// request's From (e.g. "Areafix"), which never resolves to a real
// local account -- Inbox alone (filtered to one specific recipient)
// would never surface it to anyone, so UnresolvedInbox must find it
// by its unresolved state instead.
func TestUnresolvedInboxFindsMessagesWithNoMatchingLocalUser(t *testing.T) {
	s, users := newTestStore(t)
	bob, err := users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}

	written := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)
	if _, err := s.Receive("Areafix", "21:3/100", 0, "Areafix", "", "Re: %LIST", "area list here", written, false); err != nil {
		t.Fatalf("Receive (unresolved): %v", err)
	}
	// A message addressed to a real, resolved local user must not
	// show up here -- it already has its own place, bob's own Inbox.
	if _, err := s.Receive("Someone", "21:3/200", bob.ID, "bob", "", "Hello", "hi", written, false); err != nil {
		t.Fatalf("Receive (resolved): %v", err)
	}

	got, err := s.UnresolvedInbox(10)
	if err != nil {
		t.Fatalf("UnresolvedInbox: %v", err)
	}
	if len(got) != 1 || got[0].Subject != "Re: %LIST" {
		t.Fatalf("UnresolvedInbox = %+v, want exactly the one unresolved message", got)
	}
}

func TestCountUnresolvedInboxIsNotCappedByAListLimit(t *testing.T) {
	s, _ := newTestStore(t)
	written := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if _, err := s.Receive("Areafix", "21:3/100", 0, "Areafix", "", "Re: %LIST", "area list here", written, false); err != nil {
			t.Fatalf("Receive: %v", err)
		}
	}

	got, err := s.UnresolvedInbox(2)
	if err != nil {
		t.Fatalf("UnresolvedInbox: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("UnresolvedInbox(2) len = %d, want 2 (the list cap)", len(got))
	}

	n, err := s.CountUnresolvedInbox()
	if err != nil {
		t.Fatalf("CountUnresolvedInbox: %v", err)
	}
	if n != 3 {
		t.Fatalf("CountUnresolvedInbox = %d, want the true total 3, not capped like the list", n)
	}
}

func TestDeleteRemovesMessageFromInbox(t *testing.T) {
	s, users := newTestStore(t)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	bob, err := users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}
	msg, err := s.Send(alice.ID, "", bob.ID, "bob", "", "Hi", "hi bob", false)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if err := s.Delete(msg.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	inbox, err := s.Inbox(bob.ID)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(inbox) != 0 {
		t.Fatalf("bob's inbox after Delete = %+v, want empty", inbox)
	}
}

func TestDeleteOfAbsentMessageIsNotAnError(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Delete(999999); err != nil {
		t.Fatalf("Delete of an absent message: %v, want nil", err)
	}
}
