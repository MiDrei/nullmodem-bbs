package bbs

import (
	"strings"
	"testing"
	"time"
)

func TestThreadStepFollowsReplies(t *testing.T) {
	s := testServer(t)
	area, _ := s.Messages.CreateArea("thr", "Threads", "", "", 0, 0)
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	recv := func(subject, msgid, reply string, mins int) int64 {
		m, _, err := s.Messages.ReceiveEcho(area.ID, "X", subject, "b", msgid, at.Add(time.Duration(mins)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Messages.Thread(m, reply); err != nil {
			t.Fatal(err)
		}
		return m.ID
	}
	root := recv("Topic", "1:1/1 1", "", 0)
	other := recv("Elsewhere", "1:1/1 2", "", 1)
	reply := recv("Re: Topic", "1:1/1 3", "1:1/1 1", 2)
	msgs, _ := s.Messages.ListMessages(area.ID)
	idx := map[int64]int{}
	for i, m := range msgs {
		idx[m.ID] = i
	}
	// ] from the topic skips the unrelated message in between.
	if j, ok := s.threadStep(msgs, idx[root], 1); !ok || msgs[j].ID != reply {
		t.Fatalf("] from root -> %v %v", ok, msgs[j].ID)
	}
	if j, ok := s.threadStep(msgs, idx[reply], -1); !ok || msgs[j].ID != root {
		t.Fatalf("[ from reply -> %v %v", ok, msgs[j].ID)
	}
	if _, ok := s.threadStep(msgs, idx[other], 1); ok {
		t.Fatal("a lone message has no thread to step in")
	}
}

func TestMessageListThreadView(t *testing.T) {
	s := testServer(t)
	u, _ := s.Users.Register("alice", "password123", 10)
	area, _ := s.Messages.CreateArea("thv", "Thread View", "", "", 0, 0)
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	post := func(subject, msgid, reply string, mins int) {
		m, _, _ := s.Messages.ReceiveEcho(area.ID, "Bob", subject, "text of "+subject, msgid, at.Add(time.Duration(mins)*time.Minute))
		s.Messages.Thread(m, reply)
	}
	post("Topic A", "1:1/1 1", "", 0)
	post("Topic B", "1:1/1 2", "", 1)
	post("Re: Topic A", "1:1/1 3", "1:1/1 1", 2)

	// T: the threads (A first, its reply is the latest); Enter reads A
	// and its reply in order; Q twice back out.
	conn := newFakeConn("T\rNQQ")
	term := NewTerminal(conn)
	term.Node = 1
	if err := s.browseArea(term, u, area); err != nil {
		t.Fatal(err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "(2) Topic A") || !strings.Contains(out, "(1) Topic B") || !strings.Contains(out, "[T] All messages") {
		t.Fatalf("thread rows missing:\n%q", out)
	}
	if strings.Index(out, "(2) Topic A") > strings.Index(out, "(1) Topic B") {
		t.Error("the thread with the latest activity should come first")
	}
	read, _ := s.Messages.ReadMessageIDs(u.ID, area.ID)
	if len(read) != 2 {
		t.Errorf("read %d messages, want Topic A and its reply", len(read))
	}
}
