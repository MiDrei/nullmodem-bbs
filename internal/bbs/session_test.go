package bbs

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.maik.ch/swissmaik/nullmodem/internal/db"
	"git.maik.ch/swissmaik/nullmodem/internal/file"
	"git.maik.ch/swissmaik/nullmodem/internal/menu"
	"git.maik.ch/swissmaik/nullmodem/internal/message"
	"git.maik.ch/swissmaik/nullmodem/internal/session"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

func testMenus() menu.Set {
	return menu.Set{
		"main": &menu.Menu{
			Name:  "main",
			Title: "Main Menu",
			Items: []menu.Item{
				{Key: "M", Label: "Message areas", Action: "builtin:areas", MinSL: 0},
				{Key: "F", Label: "File areas", Action: "builtin:files", MinSL: 0},
				{Key: "V", Label: "Version", Action: "builtin:version", MinSL: 0},
				{Key: "S", Label: "Sysop menu", Action: "goto:sysop", MinSL: 200},
				{Key: "Q", Label: "Quit", Action: "logoff", MinSL: 0},
			},
		},
		"sysop": &menu.Menu{
			Name:  "sysop",
			Title: "Sysop Menu",
			Items: []menu.Item{
				{Key: "L", Label: "List users", Action: "builtin:listusers", MinSL: 200},
				{Key: "S", Label: "Set user security level", Action: "builtin:setsl", MinSL: 200},
				{Key: "C", Label: "Create message area", Action: "builtin:createarea", MinSL: 200},
				{Key: "A", Label: "Create file area", Action: "builtin:createfilearea", MinSL: 200},
				{Key: "I", Label: "Import file", Action: "builtin:importfile", MinSL: 200},
				{Key: "M", Label: "Back to main menu", Action: "goto:main", MinSL: 0},
				{Key: "Q", Label: "Quit", Action: "logoff", MinSL: 0},
			},
		},
	}
}

// testServer builds a Server backed by a fresh temp-file SQLite
// database (needed even for menu-only tests, since Nodes is now a
// DB-backed session.Store rather than an in-memory registry).
func testServer(t *testing.T) *Server {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	nodes := session.NewStore(sqlDB)
	if err := nodes.ClearAll(); err != nil {
		t.Fatalf("Nodes.ClearAll: %v", err)
	}

	return &Server{
		Nodes:    nodes,
		Menus:    testMenus(),
		Users:    user.NewStore(sqlDB),
		Messages: message.NewStore(sqlDB),
		Files:    file.NewStore(sqlDB, filepath.Join(t.TempDir(), "files")),
	}
}

func testUser(sl int) *user.User {
	return &user.User{ID: 1, Username: "tester", SecurityLevel: sl, CreatedAt: time.Now()}
}

func TestRunMenuVersionAndQuit(t *testing.T) {
	conn := newFakeConn("V\r\nQ\r\n")
	term := NewTerminal(conn)
	s := testServer(t)

	err := s.runMenu(term, testUser(0), 1, "main")
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
	s := testServer(t)

	// SL 0 can't see or select the sysop-only "S" item, so it's an
	// unknown command and the session continues to the Q quit.
	err := s.runMenu(term, testUser(0), 1, "main")
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
	s := testServer(t)

	err := s.runMenu(term, testUser(255), 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff (logoff must unwind nested goto)", err)
	}
	if strings.Contains(conn.out.String(), "Main Menu\x1b[0m\r\n  [") && strings.Count(conn.out.String(), "Main Menu") > 1 {
		t.Fatalf("main menu should not be redisplayed after logging off from a submenu: %q", conn.out.String())
	}
}

func TestSysopMenuListUsers(t *testing.T) {
	s := testServer(t)
	sysop, err := s.Users.Register("root", "password123", user.SLSysop)
	if err != nil {
		t.Fatalf("Register sysop: %v", err)
	}
	if _, err := s.Users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register alice: %v", err)
	}

	conn := newFakeConn("S\r\nL\r\nQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, sysop, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "root") || !strings.Contains(out, "alice") {
		t.Fatalf("user list missing expected accounts: %q", out)
	}
}

func TestSysopMenuSetSecurityLevel(t *testing.T) {
	s := testServer(t)
	sysop, err := s.Users.Register("root", "password123", user.SLSysop)
	if err != nil {
		t.Fatalf("Register sysop: %v", err)
	}
	target, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}

	conn := newFakeConn("S\r\nS\r\nalice\r\n50\r\nQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, sysop, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "alice is now SL 50") {
		t.Fatalf("expected confirmation message, got: %q", out)
	}

	updated, err := s.Users.ByID(target.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if updated.SecurityLevel != 50 {
		t.Fatalf("alice's SecurityLevel = %d, want 50", updated.SecurityLevel)
	}
}

func TestSysopMenuSetSecurityLevelRejectsOutOfRange(t *testing.T) {
	s := testServer(t)
	sysop, err := s.Users.Register("root", "password123", user.SLSysop)
	if err != nil {
		t.Fatalf("Register sysop: %v", err)
	}
	target, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}

	conn := newFakeConn("S\r\nS\r\nalice\r\n999\r\nQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, sysop, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), "Invalid security level") {
		t.Fatalf("expected rejection message, got: %q", conn.out.String())
	}

	unchanged, err := s.Users.ByID(target.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if unchanged.SecurityLevel != user.SLNewUser {
		t.Fatalf("alice's SecurityLevel changed to %d, want unchanged %d", unchanged.SecurityLevel, user.SLNewUser)
	}
}

func TestSysopMenuUnreachableBelowThreshold(t *testing.T) {
	s := testServer(t)
	if _, err := s.Users.Register("root", "password123", user.SLSysop); err != nil {
		t.Fatalf("Register sysop: %v", err)
	}
	regular, err := s.Users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}

	conn := newFakeConn("S\r\nQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, regular, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if strings.Contains(conn.out.String(), "Sysop Menu") {
		t.Fatalf("regular user should not reach the sysop menu: %q", conn.out.String())
	}
}

func TestRunMenuUnknownMenuNameErrors(t *testing.T) {
	conn := newFakeConn("")
	term := NewTerminal(conn)
	s := testServer(t)

	err := s.runMenu(term, testUser(0), 1, "does-not-exist")
	if err == nil {
		t.Fatal("expected error for unknown menu name")
	}
}
