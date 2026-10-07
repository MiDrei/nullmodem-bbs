package message

import (
	"testing"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/db"
)

func TestBaseSubject(t *testing.T) {
	for in, want := range map[string]string{
		"Hello World":         "hello world",
		"Re: Hello  World":    "hello world",
		"RE^2: Re: hello":     "hello",
		"Re[3]: AW: hello":    "hello",
		"Reply to all":        "reply to all",
		"Regarding: the news": "regarding: the news",
	} {
		if got, _ := db.BaseSubject(in); got != want {
			t.Errorf("BaseSubject(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestThreadsLinkByReplyKludgeSubjectAndLateParent(t *testing.T) {
	s, users := newTestStore(t)
	area, _ := s.CreateArea("t", "T", "", "fsxNet", 0, 0)
	u, _ := users.Register("maik", "secret12", 10)
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	recv := func(subject, msgid, reply string, mins int) *Message {
		t.Helper()
		m, _, err := s.ReceiveEcho(area.ID, "X", subject, "body", msgid, at.Add(time.Duration(mins)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Thread(m, reply); err != nil {
			t.Fatal(err)
		}
		return m
	}
	root := recv("Hello", "21:1/1 0001", "", 0)
	a := recv("Re: Hello", "21:1/2 0001", "21:1/1 0001", 10)
	// Its parent hasn't arrived yet: no guess-free link, but adopted later.
	late := recv("Re: Other", "21:1/3 0001", "21:1/9 0009", 20)
	parent := recv("Other", "21:1/9 0009", "", 5)
	guessed := recv("Re: hello", "21:1/4 0001", "", 30)

	get := func(id int64) *Message { m, _ := s.MessageByID(id); return m }
	if m := get(a.ID); m.ReplyTo.Int64 != root.ID || m.ReplyGuess {
		t.Errorf("REPLY link: %+v", m.ReplyTo)
	}
	if m := get(late.ID); m.ReplyTo.Int64 != parent.ID || m.ReplyGuess {
		t.Errorf("late parent not adopted: %+v guess=%v", m.ReplyTo, m.ReplyGuess)
	}
	if m := get(guessed.ID); m.ReplyTo.Int64 != root.ID || !m.ReplyGuess {
		t.Errorf("subject guess: %+v", m.ReplyTo)
	}

	// A local reply, and a remote reply to it by the MSGID it went out under.
	local, _ := s.PostMessage(area.ID, u.ID, "X", "Re: Hello", "mine")
	if err := s.SetReplyTo(local.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	s.SetOutMsgID(local.ID, "21:3/194 0000abcd")
	answer := recv("Re: Hello", "21:1/5 0001", "21:3/194 0000abcd", 60)
	if m := get(answer.ID); m.ReplyTo.Int64 != local.ID {
		t.Errorf("reply to our own post: %+v", m.ReplyTo)
	}

	th, err := s.ThreadOf(answer.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got []int64
	var depth []int
	for _, e := range th {
		got, depth = append(got, e.ID), append(depth, e.Depth)
	}
	// root > a > local > answer, then guessed (root's later reply).
	want := []int64{root.ID, a.ID, local.ID, answer.ID, guessed.ID}
	if len(got) != len(want) {
		t.Fatalf("thread = %v (depths %v), want %v", got, depth, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("thread = %v, want %v", got, want)
		}
	}
	if depth[3] != 3 || depth[4] != 1 {
		t.Errorf("depths = %v", depth)
	}

	threads, total, err := s.AreaThreads(area.ID, u.ID, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(threads) != 2 {
		t.Fatalf("threads = %d/%d", len(threads), total)
	}
	// "Hello" was active last (the answer at +60).
	if threads[0].Root.ID != root.ID || threads[0].Replies != 4 || threads[0].Unread != 4 || threads[0].LastAt.IsZero() {
		t.Errorf("first thread = %+v", threads[0])
	}
	if threads[1].Root.ID != parent.ID || threads[1].Replies != 1 {
		t.Errorf("second thread = %+v", threads[1])
	}
}
