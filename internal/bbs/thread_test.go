package bbs

import (
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
