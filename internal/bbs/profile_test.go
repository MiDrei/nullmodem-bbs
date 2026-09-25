package bbs

import (
	"errors"
	"strings"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/user"
)

// registerProfileUser registers a throwaway first account (which
// Register always promotes to sysop) and then the non-sysop account
// the test actually drives.
func registerProfileUser(t *testing.T, s *Server) *user.User {
	t.Helper()
	if _, err := s.Users.Register("firstsysop", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := s.Users.SetRealName(u.ID, "Alice Example"); err != nil {
		t.Fatalf("SetRealName: %v", err)
	}
	u.RealName = "Alice Example"
	return u
}

func TestProfileShowsOverviewAndChangesRealName(t *testing.T) {
	s := testServer(t)
	u := registerProfileUser(t, s)

	// Reserved name rejected, then a valid one saved, then Q.
	conn := newFakeConn("R\r\nSysop\r\nAlice Changed\r\nQ\r\n")
	if err := s.showProfile(NewTerminal(conn), u); err != nil {
		t.Fatalf("showProfile: %v", err)
	}
	out := conn.out.String()
	for _, want := range []string{"Handle:         alice", "Real name:      Alice Example", "not set (times shown in UTC)", "That name is reserved.", "Real name saved."} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	stored, err := s.Users.ByID(u.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if stored.RealName != "Alice Changed" || u.RealName != "Alice Changed" {
		t.Fatalf("real name stored=%q session=%q, want Alice Changed", stored.RealName, u.RealName)
	}
}

func TestProfileTimezoneFromListOtherAndUnset(t *testing.T) {
	s := testServer(t)
	u := registerProfileUser(t, s)
	term := NewTerminal(newFakeConn(
		// Pick Europe/Zurich by number.
		"T\r\n4\r\n" +
			// Unknown name, then a valid free-text one.
			"T\r\nO\r\nMars/Olympus\r\nO\r\nAsia/Tokyo\r\n" +
			"Q\r\n"))
	if err := s.showProfile(term, u); err != nil {
		t.Fatalf("showProfile: %v", err)
	}
	out := term.conn.(*fakeConn).out.String()
	if !strings.Contains(out, "Time zone saved: Europe/Zurich") || !strings.Contains(out, "Unknown time zone: Mars/Olympus") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	stored, _ := s.Users.ByID(u.ID)
	if stored.Timezone != "Asia/Tokyo" || u.Timezone != "Asia/Tokyo" {
		t.Fatalf("timezone stored=%q session=%q, want Asia/Tokyo", stored.Timezone, u.Timezone)
	}
	// The session's own display zone follows immediately.
	if got := term.Time(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)).Format("15:04 MST"); got != "09:00 JST" {
		t.Fatalf("session time = %q, want 09:00 JST", got)
	}

	term = NewTerminal(newFakeConn("T\r\nN\r\nQ\r\n"))
	if err := s.showProfile(term, u); err != nil {
		t.Fatalf("showProfile: %v", err)
	}
	stored, _ = s.Users.ByID(u.ID)
	if stored.Timezone != "" {
		t.Fatalf("timezone after N = %q, want unset", stored.Timezone)
	}
}

func TestProfileChangePassword(t *testing.T) {
	s := testServer(t)
	u := registerProfileUser(t, s)

	conn := newFakeConn(
		// Wrong current password.
		"P\r\nwrong\r\nnewpass1\r\nnewpass1\r\n" +
			// Mismatched confirmation.
			"P\r\npassword123\r\nnewpass1\r\nnewpass2\r\n" +
			// Too short.
			"P\r\npassword123\r\nabc\r\nabc\r\n" +
			// Success.
			"P\r\npassword123\r\nnewpass1\r\nnewpass1\r\n" +
			"Q\r\n")
	if err := s.showProfile(NewTerminal(conn), u); err != nil {
		t.Fatalf("showProfile: %v", err)
	}
	out := conn.out.String()
	for _, want := range []string{"Current password is incorrect", "Passwords did not match", "Password too short", "Password changed."} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "newpass1") {
		t.Fatalf("password echoed to the terminal:\n%s", out)
	}
	if _, err := s.Users.Authenticate("alice", "newpass1"); err != nil {
		t.Fatalf("login with new password: %v", err)
	}
	if _, err := s.Users.Authenticate("alice", "password123"); !errors.Is(err, user.ErrInvalidCredentials) {
		t.Fatalf("old password still accepted: %v", err)
	}
}

func TestStatsBuiltinOpensProfile(t *testing.T) {
	if builtins["stats"] == nil || builtins["profile"] == nil {
		t.Fatal("both builtin:stats and builtin:profile must be registered")
	}
	s := testServer(t)
	u := registerProfileUser(t, s)
	conn := newFakeConn("Q\r\n")
	if err := builtins["stats"](s, NewTerminal(conn), u); err != nil {
		t.Fatalf("stats builtin: %v", err)
	}
	if !strings.Contains(conn.out.String(), "Your profile") {
		t.Fatalf("builtin:stats did not show the profile:\n%s", conn.out.String())
	}
}

func TestSessionDatesFollowProfileTimezone(t *testing.T) {
	s := testServer(t)
	u := registerProfileUser(t, s)
	if err := s.Users.SetTimezone(u.ID, "Europe/Zurich"); err != nil {
		t.Fatalf("SetTimezone: %v", err)
	}
	u.Timezone = "Europe/Zurich"

	term := NewTerminal(newFakeConn(""))
	term.SetLocation(u.Location())
	summer := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	if got := term.Time(summer).Format("2006-01-02 15:04 MST"); got != "2026-07-01 12:00 CEST" {
		t.Fatalf("detail date = %q, want 2026-07-01 12:00 CEST", got)
	}

	// Without a profile zone: UTC, labelled as such.
	if got := NewTerminal(newFakeConn("")).Time(summer).Format("15:04 MST"); got != "10:00 UTC" {
		t.Fatalf("default = %q, want 10:00 UTC", got)
	}
}
