package bbs

import (
	"errors"
	"strings"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/menu"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestBackReturnsToTheMenuItCameFrom(t *testing.T) {
	s := testServer(t)
	s.Menus = menu.Set{
		"main": &menu.Menu{Name: "main", Title: "Main Menu", Items: []menu.Item{
			{Key: "S", Label: "Sysop menu", Action: "goto:sysop"},
			{Key: "Q", Label: "Quit", Action: "logoff"},
		}},
		"sysop": &menu.Menu{Name: "sysop", Title: "Sysop Menu", Items: []menu.Item{
			{Key: "M", Label: "Back to main menu", Action: "back"},
		}},
	}
	u, _ := s.Users.Register("maik", "password123", user.SLNewUser) // sysop
	// In and out of the sysop menu twice, then one Q is enough.
	conn := newFakeConn("S\r\nM\r\nS\r\nM\r\nQ\r\n")
	if err := s.runMenu(NewTerminal(conn), u, 1, "main"); !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu: %v", err)
	}
	if n := strings.Count(conn.out.String(), "Goodbye"); n != 1 {
		t.Fatalf("logged off %d times", n)
	}
}

func TestSysopItemOnlyForSysops(t *testing.T) {
	s := testServer(t)
	sysop, _ := s.Users.Register("maik", "password123", user.SLNewUser)
	caller, _ := s.Users.Register("bob", "password123", user.SLNewUser)
	if v := s.userVars(sysop, 1)["SYSOP_ITEM"]; !strings.Contains(v, "Sysop Menu") {
		t.Fatalf("sysop: %q", v)
	}
	if v := s.userVars(caller, 1)["SYSOP_ITEM"]; v != "" {
		t.Fatalf("caller sees %q", v)
	}
}
