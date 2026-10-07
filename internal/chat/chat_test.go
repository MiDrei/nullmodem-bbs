package chat

import (
	"strings"

	"github.com/midrei/nullmodem-bbs/internal/user"
	"path/filepath"
	"testing"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/db"
)

func testStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	s := NewStore(sqlDB)
	now := time.Date(2026, 10, 1, 20, 0, 0, 0, time.Local)
	s.now = func() time.Time { return now }
	return s, &now
}

func TestRoomLinesAndPresence(t *testing.T) {
	s, now := testStore(t)
	if err := s.Enter(Main, "bob", "node 1"); err != nil {
		t.Fatal(err)
	}
	s.Enter(Main, "maik", "web")
	s.Post(Main, "bob", "node 1", Say, "  hi there  ")
	lines, _ := s.Lines(Main, 0, 10)
	if len(lines) != 3 || lines[2].Text != "hi there" || lines[0].Kind != Join {
		t.Fatalf("lines %+v", lines)
	}
	more, _ := s.Lines(Main, lines[1].ID, 10)
	if len(more) != 1 || more[0].Username != "bob" {
		t.Fatalf("after: %+v", more)
	}
	if p, _ := s.Present(Main); len(p) != 2 {
		t.Fatalf("present %+v", p)
	}
	// The web side stops polling: gone after PresentFor.
	*now = now.Add(10 * time.Second)
	s.Touch(Main, "bob", "node 1")
	*now = now.Add(10 * time.Second)
	if p, _ := s.Present(Main); len(p) != 1 || p[0].Username != "bob" {
		t.Fatalf("present after the web left %+v", p)
	}
	s.Exit(Main, "bob", "node 1")
	if p, _ := s.Present(Main); len(p) != 0 {
		t.Fatalf("present after exit %+v", p)
	}
	if _, err := s.Post("../etc", "x", "", Say, "x"); err != ErrBadRoom {
		t.Fatal("bad room name accepted")
	}
}

func TestRoomsPutWaitingCallersFirst(t *testing.T) {
	s, _ := testStore(t)
	s.Post(Main, "bob", "node 1", Say, "hello")
	room := PageRoom("Alice")
	s.Enter(room, "alice", "node 2")
	s.Post(room, "alice", "node 2", Page, "help with QWK")
	rooms, err := s.Rooms()
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 2 || rooms[0].Name != "page-alice" || !rooms[0].Paging || rooms[1].Name != Main {
		t.Fatalf("rooms %+v", rooms)
	}
}

func TestOneliners(t *testing.T) {
	s, _ := testStore(t)
	if _, err := s.db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'bob', 'x')`); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"first", "second", "a line that is far too long to fit on the wall beside the name of its writer"} {
		if _, err := s.AddOneliner(1, "bob", text); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AddOneliner(1, "bob", "   "); err == nil {
		t.Fatal("empty one-liner accepted")
	}
	list, _ := s.Oneliners(2)
	if len(list) != 2 || list[0].Text != "second" || len(list[1].Text) != MaxOneliner {
		t.Fatalf("wall %+v", list)
	}
	s.DeleteOneliner(list[0].ID)
	if list, _ = s.Oneliners(10); len(list) != 2 {
		t.Fatalf("after delete %+v", list)
	}
}

func TestQuietSysopsAndClear(t *testing.T) {
	s, _ := testStore(t)
	users := user.NewStore(s.db)
	users.Register("SwissMaik", "password123", user.SLSysop)
	users.Register("bob", "password123", user.SLNewUser)
	announce := false
	s.Quiet = QuietSysops(users, func() bool { return announce })

	s.Enter(Main, "SwissMaik", "web")
	s.Enter(Main, "bob", "node 1")
	s.Post(Main, "SwissMaik", "web", Say, "hello")
	s.Exit(Main, "SwissMaik", "web")
	lines, _ := s.Lines(Main, 0, 50)
	var kinds []string
	for _, l := range lines {
		kinds = append(kinds, l.Username+":"+l.Kind)
	}
	if got := strings.Join(kinds, " "); got != "bob:join SwissMaik:say" {
		t.Fatalf("lines %q, want the sysop's enter and leave unsaid", got)
	}
	announce = true
	s.Enter(Main, "SwissMaik", "web")
	if lines, _ := s.Lines(Main, 0, 50); lines[len(lines)-1].Kind != Join {
		t.Fatalf("announced sysop not announced: %+v", lines)
	}

	s.Post("other", "bob", "node 1", Say, "elsewhere")
	if n, err := s.Clear(Main); err != nil || n != 3 {
		t.Fatalf("Clear = %d, %v", n, err)
	}
	if lines, _ := s.Lines(Main, 0, 50); len(lines) != 0 {
		t.Fatalf("lines left: %+v", lines)
	}
	if lines, _ := s.Lines("other", 0, 50); len(lines) != 1 {
		t.Fatal("another room's lines went too")
	}
	if p, _ := s.Present(Main); len(p) != 2 {
		t.Fatalf("presence changed: %+v", p)
	}
}
