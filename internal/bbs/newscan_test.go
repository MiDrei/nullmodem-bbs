package bbs

import (
	"strings"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/message"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

// scanFixture: alice reads, bob writes -- two areas with two new
// messages each, the second one's first addressed to alice.
func scanFixture(t *testing.T) (*Server, *user.User, *message.Area, *message.Area, []*message.Message) {
	t.Helper()
	s := testServer(t)
	alice, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Users.SetRealName(alice.ID, "Alice Smith"); err != nil {
		t.Fatal(err)
	}
	alice, _ = s.Users.ByUsername("alice")
	bob, _ := s.Users.Register("bob", "password123", user.SLNewUser)
	general, _ := s.Messages.AreaByTag("general")
	second, err := s.Messages.CreateArea("SECOND", "Second Area", "", "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var msgs []*message.Message
	post := func(a *message.Area, to, subject string) {
		m, err := s.Messages.PostMessage(a.ID, bob.ID, to, subject, "body of "+subject)
		if err != nil {
			t.Fatal(err)
		}
		msgs = append(msgs, m)
	}
	post(general, "All", "general one")
	post(general, "All", "general two")
	post(second, "alice smith", "second one")
	post(second, "All", "second two")
	return s, alice, general, second, msgs
}

func unreadIn(t *testing.T, s *Server, u *user.User, a *message.Area) int {
	t.Helper()
	msgs, err := s.Messages.UnreadMessages(a.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return len(msgs)
}

func TestNewScanReadsAreaAfterAreaAndSkipLeavesTheRestUnread(t *testing.T) {
	s, alice, general, second, _ := scanFixture(t)
	// general: next, next (end of area); second: S skips; end note.
	conn := newFakeConn("nns\r\n")
	if err := s.newScan(NewTerminal(conn), alice); err != nil {
		t.Fatal(err)
	}
	out := conn.out.String()
	for _, want := range []string{"general one", "general two", "second one", "End of the new messages"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	if strings.Contains(out, "second two") {
		t.Error("S showed the rest of the area")
	}
	if n := unreadIn(t, s, alice, general); n != 0 {
		t.Errorf("general: %d unread after reading it", n)
	}
	if n := unreadIn(t, s, alice, second); n != 1 {
		t.Errorf("second: %d unread after skipping it, want 1", n)
	}
}

func TestNewScanMarkAreaReadAndStop(t *testing.T) {
	s, alice, general, second, _ := scanFixture(t)
	// general: M marks the rest read; second: Q stops.
	conn := newFakeConn("mq")
	if err := s.newScan(NewTerminal(conn), alice); err != nil {
		t.Fatal(err)
	}
	if n := unreadIn(t, s, alice, general); n != 0 {
		t.Errorf("general: %d unread after M", n)
	}
	if n := unreadIn(t, s, alice, second); n != 1 {
		t.Errorf("second: %d unread after Q on its first, want 1", n)
	}
	if strings.Contains(conn.out.String(), "End of the new messages") {
		t.Error("Q went on to the end note")
	}
}

func TestNewScanFollowsTheAreaSelection(t *testing.T) {
	s, alice, general, second, _ := scanFixture(t)
	if err := s.Messages.SetQWKSelectedAreas(alice.ID, []int64{second.ID}); err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn("nn\r\n")
	if err := s.newScan(NewTerminal(conn), alice); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(conn.out.String(), "general one") {
		t.Error("an area outside the selection was scanned")
	}
	if unreadIn(t, s, alice, general) != 2 || unreadIn(t, s, alice, second) != 0 {
		t.Error("wrong areas read")
	}
}

func TestToMeReadsOnlyMessagesToTheCaller(t *testing.T) {
	s, alice, _, _, msgs := scanFixture(t)
	// One message to alice (by real name, other case): N at its end.
	conn := newFakeConn("n")
	if err := s.toMe(NewTerminal(conn), alice); err != nil {
		t.Fatal(err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "second one") || strings.Contains(out, "general one") || strings.Contains(out, "second two") {
		t.Errorf("to-me showed the wrong messages: %q", out)
	}
	read, _ := s.Messages.ReadMessageIDs(alice.ID, msgs[2].AreaID)
	if !read[msgs[2].ID] {
		t.Error("the message to alice wasn't marked read")
	}
	// Nothing left: a note.
	conn = newFakeConn("\r\n")
	if err := s.toMe(NewTerminal(conn), alice); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conn.out.String(), "No new messages to you") {
		t.Error("no note when nothing is addressed to the caller")
	}
}

func TestLoginSummaryCountsAndStartsTheScan(t *testing.T) {
	s, alice, general, second, _ := scanFixture(t)
	conn := newFakeConn("rnnnn\r\n")
	if err := s.loginSummary(NewTerminal(conn), alice); err != nil {
		t.Fatal(err)
	}
	out := conn.out.String()
	for _, want := range []string{"New since your last call", "1 message", "4 messages in 2 areas", "[R] Read new", "[T] To you"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q", want)
		}
	}
	if strings.Contains(out, "[N] Netmail") {
		t.Error("netmail offered without unread netmail")
	}
	if unreadIn(t, s, alice, general)+unreadIn(t, s, alice, second) != 0 {
		t.Error("R didn't read through the new messages")
	}

	// All read: no summary at all.
	conn = newFakeConn("")
	if err := s.loginSummary(NewTerminal(conn), alice); err != nil {
		t.Fatal(err)
	}
	if conn.out.Len() != 0 {
		t.Errorf("summary shown with nothing new: %q", conn.out.String())
	}
}
