package bbs

import (
	"sync"
	"testing"
)

func TestNodeManagerConcurrentJoinLeave(t *testing.T) {
	m := NewNodeManager()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			node := m.Join("127.0.0.1:0", "test")
			m.SetUsername(node, "user")
			_ = m.Snapshot()
			m.Leave(node)
		}()
	}
	wg.Wait()

	if got := len(m.Snapshot()); got != 0 {
		t.Fatalf("expected empty node table after all sessions left, got %d", got)
	}
}

func TestNodeManagerAssignsIncreasingNodeNumbers(t *testing.T) {
	m := NewNodeManager()
	a := m.Join("1.1.1.1:1", "ansi")
	b := m.Join("2.2.2.2:2", "vt100")

	if a == b {
		t.Fatalf("expected distinct node numbers, got %d and %d", a, b)
	}

	snap := m.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(snap))
	}
	if snap[0].Node != a || snap[1].Node != b {
		t.Fatalf("expected snapshot ordered by node number, got %+v", snap)
	}
}
