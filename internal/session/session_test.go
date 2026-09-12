package session

import (
	"path/filepath"
	"sync"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/db"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	return NewStore(sqlDB)
}

func TestClearAllRemovesStaleRows(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	if _, err := sqlDB.Exec(
		`INSERT INTO sessions (node, remote_ip, term_type, username) VALUES (1, '1.2.3.4', 'ansi', 'stale')`,
	); err != nil {
		t.Fatalf("seed stale row: %v", err)
	}

	s := NewStore(sqlDB)
	if err := s.ClearAll(); err != nil {
		t.Fatalf("ClearAll: %v", err)
	}
	nodes, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("List() = %+v, want empty after ClearAll", nodes)
	}
}

func TestNewStoreDoesNotClearExistingRows(t *testing.T) {
	// The web admin daemon calls NewStore without ClearAll, since it
	// must never wipe the BBS daemon's live session state just by
	// starting or restarting.
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	if _, err := sqlDB.Exec(
		`INSERT INTO sessions (node, remote_ip, term_type, username) VALUES (1, '1.2.3.4', 'ansi', 'alice')`,
	); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	s := NewStore(sqlDB)
	nodes, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Username != "alice" {
		t.Fatalf("List() = %+v, want the pre-existing row untouched", nodes)
	}
}

func TestJoinSetUsernameLeave(t *testing.T) {
	s := newTestStore(t)

	node, err := s.Join("127.0.0.1:1234", "ansi")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if node != 1 {
		t.Fatalf("first Join node = %d, want 1", node)
	}

	nodes, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Username != "(logging in)" {
		t.Fatalf("List() after Join = %+v, want one node awaiting login", nodes)
	}

	if err := s.SetUsername(node, "alice"); err != nil {
		t.Fatalf("SetUsername: %v", err)
	}
	nodes, err = s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if nodes[0].Username != "alice" {
		t.Fatalf("Username after SetUsername = %q, want %q", nodes[0].Username, "alice")
	}

	if err := s.Leave(node); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	nodes, err = s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("List() after Leave = %+v, want empty", nodes)
	}
}

func TestJoinAssignsIncreasingNodeNumbers(t *testing.T) {
	s := newTestStore(t)

	a, err := s.Join("1.1.1.1:1", "ansi")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	b, err := s.Join("2.2.2.2:2", "vt100")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if a == b {
		t.Fatalf("expected distinct node numbers, got %d and %d", a, b)
	}

	nodes, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(nodes) != 2 || nodes[0].Node != a || nodes[1].Node != b {
		t.Fatalf("expected List ordered by node number, got %+v", nodes)
	}
}

// TestJoinReusesFreedNodeNumbers is a regression test: repeatedly
// connecting and disconnecting one at a time must keep reusing node 1
// (a real BBS's node represents a reusable line/slot), not count up
// forever.
func TestJoinReusesFreedNodeNumbers(t *testing.T) {
	s := newTestStore(t)

	for i := 0; i < 5; i++ {
		node, err := s.Join("127.0.0.1:1234", "ansi")
		if err != nil {
			t.Fatalf("Join #%d: %v", i, err)
		}
		if node != 1 {
			t.Fatalf("Join #%d = node %d, want 1 (reused, not incremented)", i, node)
		}
		if err := s.Leave(node); err != nil {
			t.Fatalf("Leave #%d: %v", i, err)
		}
	}
}

// TestJoinFillsGapInUsedNodeNumbers covers the case where the lowest
// active node leaves while a higher one is still connected: the next
// Join must backfill the freed low number rather than continuing
// upward from the highest number ever used.
func TestJoinFillsGapInUsedNodeNumbers(t *testing.T) {
	s := newTestStore(t)

	a, err := s.Join("1.1.1.1:1", "ansi")
	if err != nil {
		t.Fatalf("Join a: %v", err)
	}
	b, err := s.Join("2.2.2.2:2", "ansi")
	if err != nil {
		t.Fatalf("Join b: %v", err)
	}
	if a != 1 || b != 2 {
		t.Fatalf("got a=%d b=%d, want 1 and 2", a, b)
	}

	if err := s.Leave(a); err != nil {
		t.Fatalf("Leave a: %v", err)
	}

	c, err := s.Join("3.3.3.3:3", "ansi")
	if err != nil {
		t.Fatalf("Join c: %v", err)
	}
	if c != 1 {
		t.Fatalf("Join c = %d, want 1 (backfilling the gap a left)", c)
	}
}

func TestConcurrentJoinLeave(t *testing.T) {
	s := newTestStore(t)
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			node, err := s.Join("127.0.0.1:0", "test")
			if err != nil {
				t.Errorf("Join: %v", err)
				return
			}
			if err := s.SetUsername(node, "user"); err != nil {
				t.Errorf("SetUsername: %v", err)
			}
			if _, err := s.List(); err != nil {
				t.Errorf("List: %v", err)
			}
			if err := s.Leave(node); err != nil {
				t.Errorf("Leave: %v", err)
			}
		}()
	}
	wg.Wait()

	nodes, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("expected empty node table after all sessions left, got %d", len(nodes))
	}
}
