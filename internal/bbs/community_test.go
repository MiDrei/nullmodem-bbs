package bbs

import (
	"strings"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/chat"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

func chatServer(t *testing.T) *Server {
	t.Helper()
	s := testServer(t)
	s.Chat = chat.NewStore(s.Users.DB())
	return s
}

func TestOnelinersAtLogin(t *testing.T) {
	s := chatServer(t)
	u, _ := s.Users.Register("alice", "password123", user.SLNewUser)
	conn := newFakeConn("y\r\nHello from the wall\r\n")
	if err := s.showOneliners(NewTerminal(conn), u); err != nil {
		t.Fatal(err)
	}
	list, _ := s.Chat.Oneliners(10)
	if len(list) != 1 || list[0].Text != "Hello from the wall" || list[0].Username != "alice" {
		t.Fatalf("wall %+v", list)
	}
	// The next caller sees it; one waiting for approval may not write.
	bob, _ := s.Users.RegisterNew("bob", "password123", 5, true)
	conn = newFakeConn("\r\n")
	s.showOneliners(NewTerminal(conn), bob)
	out := conn.out.String()
	if !strings.Contains(out, "Hello from the wall") || strings.Contains(out, "Add a one-liner") {
		t.Fatalf("waiting caller's wall: %q", out)
	}
}

func TestNodeMessagesShowAtTheNextPrompt(t *testing.T) {
	s := chatServer(t)
	s.nodeMsgs.send(2, "Message from alice (node 1): hi")
	conn := newFakeConn("")
	term := NewTerminal(conn)
	term.Node = 2
	s.showNodeMessages(term)
	if !strings.Contains(conn.out.String(), "Message from alice (node 1): hi") {
		t.Fatalf("not shown: %q", conn.out.String())
	}
	conn = newFakeConn("")
	term = NewTerminal(conn)
	term.Node = 2
	s.showNodeMessages(term)
	if conn.out.Len() != 0 {
		t.Fatal("shown twice")
	}
}

func TestChatRoomPostsAndLeavesWithoutEatingAKey(t *testing.T) {
	s := chatServer(t)
	u, _ := s.Users.Register("alice", "password123", user.SLNewUser)
	conn := newFakeConn("hello all\r/q\rX")
	term := NewTerminal(conn)
	term.Node = 1
	if err := s.chatRoom(term, u, chat.Main, "Teleconference", ""); err != nil {
		t.Fatal(err)
	}
	lines, _ := s.Chat.Lines(chat.Main, 0, 10)
	var kinds []string
	for _, l := range lines {
		kinds = append(kinds, l.Kind+":"+l.Text)
	}
	if got := strings.Join(kinds, " "); got != "join: say:hello all leave:" {
		t.Fatalf("room lines %q", got)
	}
	// The key after /q is still there for whatever comes next.
	k, err := term.ReadKey()
	if err != nil || k.Rune != 'X' {
		t.Fatalf("next key %v %v, want X", k, err)
	}
	if p, _ := s.Chat.Present(chat.Main); len(p) != 0 {
		t.Fatalf("still present after leaving: %+v", p)
	}
}

func TestPagingTellsTheSysopOnTheirNode(t *testing.T) {
	s := chatServer(t)
	sysop, _ := s.Users.Register("maik", "password123", user.SLNewUser)
	alice, _ := s.Users.Register("alice", "password123", user.SLNewUser)
	sysopNode, _ := s.Nodes.Join("10.0.0.1:1", "ansi")
	s.Nodes.SetUsername(sysopNode, sysop.Username)
	aliceNode, _ := s.Nodes.Join("10.0.0.2:1", "ansi")
	s.Nodes.SetUsername(aliceNode, alice.Username)

	conn := newFakeConn("QWK packet won't open\r\n/q\r")
	term := NewTerminal(conn)
	term.Node = aliceNode
	if err := s.pageSysop(term, alice); err != nil {
		t.Fatal(err)
	}
	lines, _ := s.Chat.Lines(chat.PageRoom("alice"), 0, 10)
	paged := false
	for _, l := range lines {
		paged = paged || (l.Kind == chat.Page && l.Text == "QWK packet won't open")
	}
	if !paged {
		t.Fatalf("no page line: %+v", lines)
	}
	msgs := s.nodeMsgs.take(sysopNode)
	if len(msgs) != 1 || !strings.Contains(msgs[0], "alice") || !strings.Contains(msgs[0], "paging") {
		t.Fatalf("sysop's node got %q", msgs)
	}
}
