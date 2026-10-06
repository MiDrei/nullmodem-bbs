package bbs

import (
	"strings"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/community"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestNewsAtLoginOnceAndInTheList(t *testing.T) {
	s := testServer(t)
	s.Community = community.NewStore(s.Users.DB())
	u, _ := s.Users.Register("alice", "password123", user.SLNewUser)
	s.Community.SaveNews(community.News{TitleEN: "Doors are back", TextEN: "Play them.", TitleDE: "Doors sind zurück", TextDE: "Spielt sie."})
	s.Community.SaveNews(community.News{TitleEN: "New look", TextEN: "Space."})

	conn := newFakeConn("\r\n")
	term := NewTerminal(conn)
	term.Lang = "de-du"
	if err := s.showNewsAtLogin(term, u); err != nil {
		t.Fatal(err)
	}
	out := plainText(conn.out.String())
	if !strings.Contains(out, "Doors sind zur") || !strings.Contains(out, "New look") || !strings.Contains(out, "2 neue Meldungen") {
		t.Fatalf("login news: %q", out)
	}

	// Seen: not again at the next login.
	conn = newFakeConn("")
	if err := s.showNewsAtLogin(NewTerminal(conn), u); err != nil || conn.out.Len() != 0 {
		t.Fatalf("shown twice: %v %q", err, conn.out.String())
	}

	// The list, newest first: Enter reads it, a key back, Q.
	conn = newFakeConn("\r\n\r\nq")
	if err := s.showNews(NewTerminal(conn), u); err != nil {
		t.Fatal(err)
	}
	out = plainText(conn.out.String())
	if i, j := strings.Index(out, "New look"), strings.Index(out, "Doors are back"); i < 0 || j < 0 || i > j {
		t.Fatalf("list: %q", out)
	}
	if !strings.Contains(out, "Space.") {
		t.Fatalf("Enter didn't show the item: %q", out)
	}
}
