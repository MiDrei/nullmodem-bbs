package bbs

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/community"
	"github.com/midrei/nullmodem-bbs/internal/nodelist"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

func TestVotingBoothVoteAndResults(t *testing.T) {
	s := testServer(t)
	s.Community = community.NewStore(s.Users.DB())
	u, _ := s.Users.Register("alice", "password123", user.SLNewUser)
	id, _ := s.Community.CreatePoll("Best door?", []string{"LORD", "TradeWars"})
	// Poll 1, vote 2, results' pause, then back.
	conn := newFakeConn("1\r\n2\r\n\r\n\r\n")
	if err := s.votingBooth(NewTerminal(conn), u); err != nil {
		t.Fatal(err)
	}
	p, _ := s.Community.Poll(id, u.ID)
	if p.MyVote != p.Options[1].ID || p.Total != 1 {
		t.Fatalf("vote not recorded: %+v", p)
	}
	out := conn.out.String()
	if !strings.Contains(out, "Results") || !strings.Contains(out, "100%") {
		t.Fatalf("no results shown: %q", out)
	}
}

func TestBBSListAddAndOnlyOwnEditable(t *testing.T) {
	s := testServer(t)
	s.Community = community.NewStore(s.Users.DB())
	s.Users.Register("maik", "password123", user.SLNewUser) // sysop
	alice, _ := s.Users.Register("alice", "password123", user.SLNewUser)
	bob, _ := s.Users.Register("bob", "password123", user.SLNewUser)
	conn := newFakeConn("AAgency BBS\r\nagency.bbs.nz:2323\r\nAvon\r\nMystic\r\nThe fsxNet hub\r\nQ")
	if err := s.bbsList(NewTerminal(conn), alice); err != nil {
		t.Fatal(err)
	}
	list, _ := s.Community.BBSList()
	if len(list) != 1 || list[0].Address != "agency.bbs.nz:2323" || list[0].AddedBy != "alice" {
		t.Fatalf("list %+v", list)
	}
	if s.mayChangeBBS(bob, list[0]) || !s.mayChangeBBS(alice, list[0]) {
		t.Fatal("wrong edit rights")
	}
}

func TestNetmailToAnAddressShowsTheNodelistEntry(t *testing.T) {
	s := testServer(t)
	s.Nodelist = nodelist.NewStore(s.Users.DB())
	s.Nodelist.Replace("fsxNet", "FSXNET.Z75", []nodelist.Entry{{Network: "fsxNet", Zone: 21, Net: 1, Node: 101, Name: "Agency BBS", Location: "Dunedin NZL", Sysop: "Paul Hayton"}})
	u, _ := s.Users.Register("alice", "password123", user.SLNewUser)
	conn := newFakeConn("21:1/101\r\n\r\n\r\n")
	s.composeNetmail(NewTerminal(conn), u)
	out := conn.out.String()
	if !strings.Contains(out, "Agency BBS, Dunedin NZL (sysop Paul Hayton)") || !strings.Contains(out, "Recipient name [Paul Hayton]") {
		t.Fatalf("no nodelist lookup: %q", out)
	}
}

func TestBBSListDetailsInGermanWrapAndKeepUmlauts(t *testing.T) {
	s := testServer(t)
	s.Community = community.NewStore(s.Users.DB())
	s.Users.Register("maik", "password123", user.SLNewUser) // sysop
	alice, _ := s.Users.Register("alice", "password123", user.SLNewUser)
	// Typed on a UTF-8 terminal: the emoji arrives as UTF-8, not as
	// four CP437 characters.
	long := "A regional Swiss BBS that ran over dial-up from 1991 to 1996. Revived from a 30-year-old backup tape. Grüezi 🙂"
	conn := newFakeConn("ABUEMA BBS\r\nbbs.buema.ch:2300\r\nMarc\r\nWildcat! v4.11\r\n" + long + "\r\nQ")
	if err := s.bbsList(NewTerminal(conn), alice); err != nil {
		t.Fatal(err)
	}
	list, _ := s.Community.BBSList()
	if len(list) != 1 || list[0].Description != long {
		t.Fatalf("stored %q", list[0].Description)
	}
	s.Community.RecordCheck(list[0].ID, true, time.Now())
	list, _ = s.Community.BBSList()

	conn = newFakeConn("\r\n")
	term := NewTerminal(conn)
	term.Lang = "de-du"
	out := &conn.out
	if err := s.showBBS(term, alice, list[0]); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "gepr\x81ft") || strings.Contains(text, "gepr?ft") {
		t.Errorf("umlaut lost: %q", text)
	}
	for _, line := range strings.Split(ansiStrip(text), "\r\n") {
		if len(line) > 79 {
			t.Errorf("line too wide (%d): %q", len(line), line)
		}
	}
	if !strings.Contains(ansiStrip(text), "Eingetragen von alice") {
		t.Errorf("labels not aligned: %q", ansiStrip(text))
	}
}

var ansiSeq = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")

func ansiStrip(s string) string { return ansiSeq.ReplaceAllString(s, "") }

func TestBBSListScrolls(t *testing.T) {
	s := testServer(t)
	s.Community = community.NewStore(s.Users.DB())
	alice, _ := s.Users.Register("alice", "password123", user.SLNewUser)
	for i := 1; i <= 30; i++ {
		s.Community.SaveBBS(community.BBS{Name: fmt.Sprintf("Board %02d", i), Address: fmt.Sprintf("board%02d.example:23", i), AddedBy: "alice"})
	}
	// End jumps to the last board, Enter shows it, a key back, Q.
	conn := newFakeConn("\x1b[F\r\n\r\nQ")
	if err := s.bbsList(NewTerminal(conn), alice); err != nil {
		t.Fatal(err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "1-") || !strings.Contains(out, "of 30") {
		t.Errorf("no scroll status: %q", out)
	}
	if strings.Count(out, "Board 30") < 2 {
		t.Errorf("End didn't reach the last board and show it: %q", out)
	}
}
