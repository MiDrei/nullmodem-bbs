package community

import (
	"path/filepath"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/db"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	for _, n := range []string{"maik", "bob"} {
		sqlDB.Exec(`INSERT INTO users (username, password_hash) VALUES (?, 'x')`, n)
	}
	return NewStore(sqlDB)
}

func TestPollVoting(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreatePoll("Lieblings-Door?", []string{"LORD", " "}); err == nil {
		t.Fatal("a poll with one option accepted")
	}
	id, err := s.CreatePoll("Lieblings-Door?", []string{"LORD", "TradeWars", "Usurper"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.Poll(id, 1)
	lord, tw := p.Options[0].ID, p.Options[1].ID
	s.Vote(id, 1, lord)
	s.Vote(id, 2, lord)
	s.Vote(id, 2, tw) // bob changes his mind
	p, _ = s.Poll(id, 2)
	if p.Total != 2 || p.Options[0].Votes != 1 || p.Options[1].Votes != 1 || p.MyVote != tw {
		t.Fatalf("results %+v", p)
	}
	if err := s.Vote(id, 1, 9999); err != ErrNotFound {
		t.Fatalf("vote for another poll's option: %v", err)
	}
	s.ClosePoll(id, true)
	if err := s.Vote(id, 1, tw); err != ErrClosed {
		t.Fatalf("vote in a closed poll: %v", err)
	}
	if open, _ := s.Polls(1, false); len(open) != 0 {
		t.Fatal("closed poll listed as open")
	}
	if all, _ := s.Polls(1, true); len(all) != 1 || all[0].MyVote != lord {
		t.Fatalf("all polls %+v", all)
	}
	s.DeletePoll(id)
	if _, err := s.Poll(id, 1); err != ErrNotFound {
		t.Fatal("deleted poll still there")
	}
}

func TestBBSList(t *testing.T) {
	s := testStore(t)
	if _, err := s.SaveBBS(BBS{Name: "x"}); err == nil {
		t.Fatal("entry without address accepted")
	}
	id, err := s.SaveBBS(BBS{Name: "Agency BBS", Address: "agency.bbs.nz:2323", Sysop: "Avon", AddedByID: 2, AddedBy: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	s.SaveBBS(BBS{Name: "Aardvark", Address: "a.example:23"})
	b, _ := s.GetBBS(id)
	b.Software = "Mystic"
	if _, err := s.SaveBBS(b); err != nil {
		t.Fatal(err)
	}
	list, _ := s.BBSList()
	if len(list) != 2 || list[0].Name != "Aardvark" || list[1].Software != "Mystic" || list[1].AddedByID != 2 {
		t.Fatalf("list %+v", list)
	}
	s.DeleteBBS(id)
	if _, err := s.GetBBS(id); err != ErrNotFound {
		t.Fatal("deleted entry still there")
	}
}
