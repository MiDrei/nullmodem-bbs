package bbs

import (
	"errors"
	"strings"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

func TestMessageAreasListAndReadSeededArea(t *testing.T) {
	s := testServerWithUsers(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	conn := newFakeConn("M\r\nQ\r\nQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "General Discussion") {
		t.Fatalf("area list missing seeded area: %q", out)
	}
}

func TestPostAndReadMessage(t *testing.T) {
	s := testServerWithUsers(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// M -> areas, "1" -> General Discussion, P -> post, subject, two
	// body lines, "." to end, re-select "1" to read it back, Q out of
	// the area, Q out of the area list, Q to log off from main.
	input := "M\r\n1\r\nP\r\nHello World\r\nLine one\r\nLine two\r\n.\r\n1\r\nQ\r\nQ\r\nQ\r\n"
	conn := newFakeConn(input)
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "Message posted.") {
		t.Fatalf("expected posting confirmation, got: %q", out)
	}
	if !strings.Contains(out, "Hello World") {
		t.Fatalf("expected subject in listing/read view, got: %q", out)
	}
	if !strings.Contains(out, "Line one") || !strings.Contains(out, "Line two") {
		t.Fatalf("expected multi-line body in read view, got: %q", out)
	}
	if !strings.Contains(out, "From:    alice") {
		t.Fatalf("expected author in read view, got: %q", out)
	}
}

func TestPostRejectedBelowWriteThreshold(t *testing.T) {
	s := testServerWithUsers(t)
	// A write-gated area (min_sl_write 100): a regular new user (SL
	// 10) can read it but must be rejected when trying to post.
	if _, err := s.Messages.CreateArea("locked", "Locked Area", "", 0, 100); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}

	// Register a throwaway first account so it (not "bob") absorbs the
	// first-user-becomes-sysop promotion, leaving bob at the requested
	// SLNewUser level.
	if _, err := s.Users.Register("bootstrap-sysop", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register bootstrap sysop: %v", err)
	}
	u, err := s.Users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Areas are listed alphabetically: "General Discussion" (seeded)
	// sorts before "Locked Area", so it's selection "2". Q out of the
	// area, Q out of the area list, Q to log off from main.
	conn := newFakeConn("M\r\n2\r\nP\r\nQ\r\nQ\r\nQ\r\n")
	term := NewTerminal(conn)

	err = s.runMenu(term, u, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff; output so far:\n%s", err, conn.out.String())
	}
	if !strings.Contains(conn.out.String(), "don't have permission to post") {
		t.Fatalf("expected permission rejection, got: %q", conn.out.String())
	}
}

func TestSysopCreateMessageArea(t *testing.T) {
	s := testServerWithUsers(t)
	sysop, err := s.Users.Register("root", "password123", user.SLSysop)
	if err != nil {
		t.Fatalf("Register sysop: %v", err)
	}

	// S -> sysop menu, C -> create area, then fields, M -> back, Q -> quit.
	input := "S\r\nC\r\ndev\r\nDev Talk\r\nFor devs\r\n0\r\n0\r\nM\r\nQ\r\n"
	conn := newFakeConn(input)
	term := NewTerminal(conn)

	err = s.runMenu(term, sysop, 1, "main")
	if !errors.Is(err, errLogoff) {
		t.Fatalf("runMenu error = %v, want errLogoff", err)
	}
	if !strings.Contains(conn.out.String(), `Area "Dev Talk" created.`) {
		t.Fatalf("expected creation confirmation, got: %q", conn.out.String())
	}

	area, err := s.Messages.AreaByTag("dev")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if area.Name != "Dev Talk" {
		t.Fatalf("area.Name = %q, want %q", area.Name, "Dev Talk")
	}
}
