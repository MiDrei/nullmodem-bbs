package bbs

import (
	"strings"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestSearchMessagesListsAndReadsResults(t *testing.T) {
	s := testServer(t)
	bob, _ := s.Users.Register("bob", "password123", user.SLNewUser)
	alice, _ := s.Users.Register("alice", "password123", user.SLNewUser)
	general, _ := s.Messages.AreaByTag("general")
	s.Messages.PostMessage(general.ID, bob.ID, "All", "MRC setup", "how to configure the bridge")
	s.Messages.PostMessage(general.ID, bob.ID, "All", "Doors", "MRC is a door too")
	s.Messages.PostMessage(general.ID, bob.ID, "All", "Weather", "sunny")

	// Search, open result 1, N to the next (the end), back, Q.
	conn := newFakeConn("mrc\r\n1\r\nnn" + "Q\r\n")
	if err := s.searchMessages(NewTerminal(conn), alice); err != nil {
		t.Fatal(err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "MRC setup") || !strings.Contains(out, "Doors") || strings.Contains(out, "Weather") {
		t.Fatalf("results: %q", out)
	}
	if !strings.Contains(out, "how to configure the bridge") || !strings.Contains(out, "MRC is a door too") {
		t.Fatal("the results weren't read through")
	}
}
