package bbs

import (
	"errors"
	"strings"
	"testing"
	"time"

	"git.maik.ch/swissmaik/nullmodem/internal/menu"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

func testMenus() menu.Set {
	return menu.Set{
		"main": &menu.Menu{
			Name:  "main",
			Title: "Main Menu",
			Items: []menu.Item{
				{Key: "V", Label: "Version", Action: "builtin:version", MinSL: 0},
				{Key: "S", Label: "Sysop menu", Action: "goto:sysop", MinSL: 200},
				{Key: "Q", Label: "Quit", Action: "logoff", MinSL: 0},
			},
		},
		"sysop": &menu.Menu{
			Name:  "sysop",
			Title: "Sysop Menu",
			Items: []menu.Item{
				{Key: "Q", Label: "Quit", Action: "logoff", MinSL: 0},
			},
		},
	}
}

func testServer() *Server {
	return &Server{
		Nodes: NewNodeManager(),
		Menus: testMenus(),
	}
}

func testUser(sl int) *user.User {
	return &user.User{ID: 1, Username: "tester", SecurityLevel: sl, CreatedAt: time.Now()}
}

func TestRunMenuVersionAndQuit(t *testing.T) {
	conn := newFakeConn("V\r\nQ\r\n")
	term := NewTerminal(conn)
	s := testServer()

	err := s.runMenu(term, testUser(0), "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), Version) {
		t.Fatalf("output missing version string: %q", conn.out.String())
	}
	if !strings.Contains(conn.out.String(), "Goodbye") {
		t.Fatalf("output missing goodbye message: %q", conn.out.String())
	}
}

func TestRunMenuGatesItemsBySecurityLevel(t *testing.T) {
	conn := newFakeConn("S\r\nQ\r\n")
	term := NewTerminal(conn)
	s := testServer()

	// SL 0 can't see or select the sysop-only "S" item, so it's an
	// unknown command and the session continues to the Q quit.
	err := s.runMenu(term, testUser(0), "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), "Unknown command") {
		t.Fatalf("expected 'Unknown command' for gated item, got: %q", conn.out.String())
	}
	if strings.Contains(conn.out.String(), "Sysop Menu") {
		t.Fatalf("sysop submenu should not have been reachable at SL 0: %q", conn.out.String())
	}
}

func TestRunMenuLogoffFromNestedGotoEndsSession(t *testing.T) {
	// At SL 255 the sysop item is visible; selecting it goes into the
	// "sysop" submenu, and quitting from there must end the whole
	// session rather than just popping back to "main".
	conn := newFakeConn("S\r\nQ\r\n")
	term := NewTerminal(conn)
	s := testServer()

	err := s.runMenu(term, testUser(255), "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff (logoff must unwind nested goto)", err)
	}
	if strings.Contains(conn.out.String(), "Main Menu\x1b[0m\r\n  [") && strings.Count(conn.out.String(), "Main Menu") > 1 {
		t.Fatalf("main menu should not be redisplayed after logging off from a submenu: %q", conn.out.String())
	}
}

func TestRunMenuUnknownMenuNameErrors(t *testing.T) {
	conn := newFakeConn("")
	term := NewTerminal(conn)
	s := testServer()

	err := s.runMenu(term, testUser(0), "does-not-exist")
	if err == nil {
		t.Fatal("expected error for unknown menu name")
	}
}
