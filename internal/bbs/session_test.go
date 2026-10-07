package bbs

import (
	"errors"
	"fmt"
	"git.maik.ch/nullmodem/bbs/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/applog"
	"git.maik.ch/nullmodem/bbs/internal/db"
	"git.maik.ch/nullmodem/bbs/internal/file"
	"git.maik.ch/nullmodem/bbs/internal/menu"
	"git.maik.ch/nullmodem/bbs/internal/message"
	"git.maik.ch/nullmodem/bbs/internal/netmail"
	"git.maik.ch/nullmodem/bbs/internal/session"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

func testMenus() menu.Set {
	return menu.Set{
		"main": &menu.Menu{
			Name:  "main",
			Title: "Main Menu",
			Items: []menu.Item{
				{Key: "M", Label: "Message areas", Action: "builtin:areas", MinSL: 0},
				{Key: "F", Label: "File areas", Action: "builtin:files", MinSL: 0},
				{Key: "N", Label: "Netmail", Action: "builtin:netmail", MinSL: 0},
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
		Netmail:  netmail.NewStore(sqlDB),
		Logger:   applog.NewLogger(applog.NewStore(sqlDB), "bbs"),
	}
}

func testUser(sl int) *user.User {
	return &user.User{ID: 1, Username: "tester", SecurityLevel: sl, CreatedAt: time.Now()}
}

func TestRunMenuVersionAndQuit(t *testing.T) {
	// The extra blank line answers showVersion's "Press Enter to
	// continue..." pause (see pauseForKey).
	conn := newFakeConn("V\r\n\r\nQ\r\n")
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

func TestLogoffUsesCustomScreenWhenPresent(t *testing.T) {
	conn := newFakeConn("Q\r\n")
	term := NewTerminal(conn)
	s := testServer(t)

	dir := t.TempDir()
	s.ScreensDir = dir
	if err := os.WriteFile(filepath.Join(dir, "logoff.ans"), []byte("So long, {USERNAME}!"), 0o644); err != nil {
		t.Fatalf("write logoff.ans: %v", err)
	}

	err := s.runMenu(term, testUser(0), 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), "So long, tester!") {
		t.Fatalf("expected rendered logoff screen, got: %q", conn.out.String())
	}
	if strings.Contains(conn.out.String(), "Goodbye,") {
		t.Fatalf("custom logoff screen should replace the plain goodbye line, got: %q", conn.out.String())
	}
}

func TestLogoffFallsBackToPlainMessageWhenScreenMissing(t *testing.T) {
	conn := newFakeConn("Q\r\n")
	term := NewTerminal(conn)
	s := testServer(t)
	s.ScreensDir = t.TempDir()

	err := s.runMenu(term, testUser(0), 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), "Goodbye, tester!") {
		t.Fatalf("expected fallback goodbye message, got: %q", conn.out.String())
	}
}

func TestRunMenuUsesCustomScreenWhenSet(t *testing.T) {
	conn := newFakeConn("Q\r\n")
	term := NewTerminal(conn)
	s := testServer(t)

	dir := t.TempDir()
	s.ScreensDir = dir
	screen := "Welcome to {BBSNAME}, {USERNAME}!"
	if err := os.WriteFile(filepath.Join(dir, "main.ans"), []byte(screen), 0o644); err != nil {
		t.Fatalf("write screen: %v", err)
	}
	s.Menus.(menu.Set)["main"].Screen = "main.ans"
	s.BBSName = "Test BBS"

	err := s.runMenu(term, testUser(0), 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), "Welcome to Test BBS, tester!") {
		t.Fatalf("expected rendered custom screen, got: %q", conn.out.String())
	}
	if strings.Contains(conn.out.String(), "  [") {
		t.Fatalf("custom screen should replace the generated item list, got: %q", conn.out.String())
	}
}

func TestRunMenuFallsBackToGeneratedListWhenScreenMissing(t *testing.T) {
	conn := newFakeConn("Q\r\n")
	term := NewTerminal(conn)
	s := testServer(t)
	s.ScreensDir = t.TempDir()
	s.Menus.(menu.Set)["main"].Screen = "does-not-exist.ans"

	err := s.runMenu(term, testUser(0), 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), "Main Menu") {
		t.Fatalf("expected fallback to the generated menu text, got: %q", conn.out.String())
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

	// The extra blank line after L answers sysopListUsers' "Press
	// Enter to continue..." pause (see pauseForKey).
	conn := newFakeConn("S\r\nL\r\n\r\nQ\r\n")
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

	conn := newFakeConn("S\r\nS\r\nalice\r\n50\r\n\r\nQ\r\n")
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

	conn := newFakeConn("S\r\nS\r\nalice\r\n999\r\n\r\nQ\r\n")
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

func TestSysopMenuSetSecurityLevelRefusesLastSysopDemotion(t *testing.T) {
	s := testServer(t)
	sysop, err := s.Users.Register("root", "password123", user.SLSysop)
	if err != nil {
		t.Fatalf("Register sysop: %v", err)
	}

	conn := newFakeConn("S\r\nS\r\nroot\r\n100\r\n\r\nQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, sysop, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), "Cannot demote the last sysop-level account") {
		t.Fatalf("expected last-sysop rejection message, got: %q", conn.out.String())
	}

	unchanged, err := s.Users.ByID(sysop.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if unchanged.SecurityLevel != user.SLSysop {
		t.Fatalf("root's SecurityLevel changed to %d, want unchanged %d", unchanged.SecurityLevel, user.SLSysop)
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

// TestHandleLogsConnectLoginAndDisconnect exercises a full connection
// through Handle (rather than calling runMenu directly, like the
// other tests here) specifically to verify activity logging, which
// Handle -- not runMenu -- is responsible for.
func TestHandleLogsConnectLoginAndDisconnect(t *testing.T) {
	dir := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	nodes := session.NewStore(sqlDB)
	if err := nodes.ClearAll(); err != nil {
		t.Fatalf("Nodes.ClearAll: %v", err)
	}
	logStore := applog.NewStore(sqlDB)

	s := &Server{
		Nodes:  nodes,
		Menus:  testMenus(),
		Users:  user.NewStore(sqlDB),
		Logger: applog.NewLogger(logStore, "bbs"),
	}

	conn := newFakeConn("\r\nalice\r\nY\r\npassword123\r\npassword123\r\nAlice Example\r\nQ\r\n")
	s.Handle(conn)

	entries, err := logStore.Recent(50)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	var messages []string
	for _, e := range entries {
		messages = append(messages, e.Message)
	}
	joined := strings.Join(messages, "\n")

	for _, want := range []string{"connected from", "alice logged in", "new account registered: alice", "disconnected"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("log entries missing %q; got:\n%s", want, joined)
		}
	}
}

// TestRegisterNewRequiresRealName locks in promptRealName's two
// rejection cases -- blank (many FTN networks reject a handle-only
// participant) and a reserved system/staff role (see
// user.IsRestrictedRealName) -- both must re-prompt rather than fail
// or hang, and a valid real name on a later attempt must still get
// through and stored.
func TestRegisterNewRequiresRealName(t *testing.T) {
	dir := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	nodes := session.NewStore(sqlDB)
	if err := nodes.ClearAll(); err != nil {
		t.Fatalf("Nodes.ClearAll: %v", err)
	}
	users := user.NewStore(sqlDB)
	s := &Server{
		Nodes:  nodes,
		Menus:  testMenus(),
		Users:  users,
		Logger: applog.NewLogger(applog.NewStore(sqlDB), "bbs"),
	}

	// Blank, then a reserved name, then a real one -- both rejections
	// must simply re-prompt.
	conn := newFakeConn("\r\nalice\r\nY\r\npassword123\r\npassword123\r\n\r\nSysop\r\nAlice Example\r\nQ\r\n")
	s.Handle(conn)

	alice, err := users.ByUsername("alice")
	if err != nil {
		t.Fatalf("ByUsername: %v", err)
	}
	if alice.RealName != "Alice Example" {
		t.Fatalf("RealName = %q, want %q", alice.RealName, "Alice Example")
	}
}

// TestLoginRejectsReservedHandleForNewRegistration locks in that a
// caller can't register a brand-new account under a reserved system/
// staff handle (see user.IsRestrictedUsername) -- the handle prompt
// must simply loop, not fail or hang.
func TestLoginRejectsReservedHandleForNewRegistration(t *testing.T) {
	dir := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	nodes := session.NewStore(sqlDB)
	if err := nodes.ClearAll(); err != nil {
		t.Fatalf("Nodes.ClearAll: %v", err)
	}
	users := user.NewStore(sqlDB)
	s := &Server{
		Nodes:  nodes,
		Menus:  testMenus(),
		Users:  users,
		Logger: applog.NewLogger(applog.NewStore(sqlDB), "bbs"),
	}

	conn := newFakeConn("\r\nadmin\r\nalice\r\nY\r\npassword123\r\npassword123\r\nAlice Example\r\nQ\r\n")
	s.Handle(conn)

	if _, err := users.ByUsername("admin"); !errors.Is(err, user.ErrNotFound) {
		t.Fatalf("ByUsername(admin) = %v, want ErrNotFound -- reserved handle must never register", err)
	}
	if _, err := users.ByUsername("alice"); err != nil {
		t.Fatalf("ByUsername(alice): %v, want the retry to succeed", err)
	}
}

// TestHandleLogsMenuErrors locks in a real production fix: an
// unexpected error bubbling up out of runMenu used to be shown to the
// caller and nowhere else, leaving the sysop with no server-side
// trail of it (an actual "mark message read" DB error was only
// discovered from a user's screenshot, never from the log). Handle
// must log it in addition to displaying it.
func TestHandleLogsMenuErrors(t *testing.T) {
	dir := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	nodes := session.NewStore(sqlDB)
	if err := nodes.ClearAll(); err != nil {
		t.Fatalf("Nodes.ClearAll: %v", err)
	}
	logStore := applog.NewStore(sqlDB)

	// A menu item pointing at a nonexistent submenu is a simple,
	// deterministic way to make runMenu return a real error (see its
	// "menu %q not found" case) without needing to break the database.
	brokenMenus := menu.Set{
		"main": &menu.Menu{
			Name:  "main",
			Title: "Main Menu",
			Items: []menu.Item{
				{Key: "B", Label: "Broken", Action: "goto:doesnotexist", MinSL: 0},
			},
		},
	}

	s := &Server{
		Nodes:  nodes,
		Menus:  brokenMenus,
		Users:  user.NewStore(sqlDB),
		Logger: applog.NewLogger(logStore, "bbs"),
	}

	conn := newFakeConn("\r\nalice\r\nY\r\npassword123\r\npassword123\r\nAlice Example\r\nB\r\n")
	s.Handle(conn)

	entries, err := logStore.Recent(50)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	var messages []string
	for _, e := range entries {
		messages = append(messages, e.Message)
	}
	joined := strings.Join(messages, "\n")
	if !strings.Contains(joined, "menu error") || !strings.Contains(joined, `menu "doesnotexist" not found`) {
		t.Fatalf("log entries missing the menu error; got:\n%s", joined)
	}

	if !strings.Contains(conn.out.String(), "Menu error:") {
		t.Fatalf("expected the error to still be shown on-screen too, got: %q", conn.out.String())
	}
}

// TestLoginLanguageChosenFirst: the language picked before logging in
// is the login's and a new account's, without being asked again.
func TestLoginLanguageChosenFirst(t *testing.T) {
	dir := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	nodes := session.NewStore(sqlDB)
	users := user.NewStore(sqlDB)
	s := &Server{
		Nodes:    nodes,
		Menus:    testMenus(),
		Users:    users,
		Logger:   applog.NewLogger(applog.NewStore(sqlDB), "bbs"),
		Language: func() string { return "de-du" },
	}
	conn := newFakeConn("1alice\r\nY\r\npassword123\r\npassword123\r\nAlice Example\r\nQ\r\n")
	s.Handle(conn)
	out := conn.out.String()
	if !strings.Contains(plainText(out), "[Enter] Deutsch (Du)") || !strings.Contains(out, "Enter your handle") {
		t.Fatalf("no English login after choosing it:\n%q", out)
	}
	alice, err := users.ByUsername("alice")
	if err != nil {
		t.Fatalf("ByUsername: %v", err)
	}
	if alice.Language != "en" {
		t.Errorf("Language = %q, want en", alice.Language)
	}
}

func TestSysopListUsersPages(t *testing.T) {
	s := testServer(t)
	root, _ := s.Users.Register("root", "password123", user.SLSysop)
	for i := 1; i <= 40; i++ {
		s.Users.Register(fmt.Sprintf("user%02d", i), "password123", 10)
	}
	// The first page, Enter for the second, then Q.
	conn := newFakeConn("\r\nq\r\n")
	if err := s.sysopListUsers(NewTerminal(conn), root); err != nil {
		t.Fatal(err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "1-") || !strings.Contains(out, "of 41") || strings.Count(out, "\x1b[2J") != 2 {
		t.Errorf("not two pages: %q", out)
	}
}

func TestSetSecurityLevelListsTheNamedLevels(t *testing.T) {
	s := testServer(t)
	root, _ := s.Users.Register("root", "password123", user.SLSysop)
	s.Users.Register("alice", "password123", 10)
	s.SecurityLevels = func(func(string) string) []config.SecurityLevel {
		return []config.SecurityLevel{{Level: 10, Name: "Neuer Benutzer"}, {Level: 20, Name: "Regulärer Benutzer"}, {Level: 255, Name: "Sysop"}}
	}
	conn := newFakeConn("alice\r\n20\r\n")
	if err := s.sysopSetSecurityLevel(NewTerminal(conn), root); err != nil {
		t.Fatal(err)
	}
	out := plainText(conn.out.String())
	if !strings.Contains(out, "20  Regul") || !strings.Contains(out, "255  Sysop") {
		t.Fatalf("no named levels: %q", out)
	}
	if u, _ := s.Users.ByUsername("alice"); u.SecurityLevel != 20 {
		t.Fatalf("level %d", u.SecurityLevel)
	}
}
