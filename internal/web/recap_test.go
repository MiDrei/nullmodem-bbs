package web

import (
	"strings"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/stats"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestMonthlyRecap(t *testing.T) {
	srv, users, _ := newTestServer(t)
	srv.Stats = stats.NewStore(srv.DB)
	root, _ := users.Register("root", "supersecret", user.SLSysop)
	caller, _ := users.Register("caller", "password123", user.SLNewUser)
	srv.Stats.RecordCall(caller.ID, "telnet")
	srv.Stats.RecordCall(root.ID, "web")

	inbox := func(id int64) []string {
		list, _ := srv.Netmail.Inbox(id)
		var out []string
		for _, m := range list {
			out = append(out, m.Subject)
		}
		return out
	}

	// Not on the 1st, not before the hour: nothing.
	srv.recapIfDue(time.Date(2026, 10, 2, 9, 0, 0, 0, time.Local))
	srv.recapIfDue(time.Date(2026, 11, 1, RecapHour-1, 0, 0, 0, time.Local))
	if got := inbox(root.ID); len(got) != 0 {
		t.Fatalf("too early: %v", got)
	}
	// The 1st: once, for the month that ended, to the sysops only.
	srv.recapIfDue(time.Date(2026, 11, 1, RecapHour, 5, 0, 0, time.Local))
	srv.recapIfDue(time.Date(2026, 11, 1, RecapHour, 15, 0, 0, time.Local))
	got := inbox(root.ID)
	if len(got) != 1 || got[0] != "Monthly recap for October 2026" {
		t.Fatalf("sysop inbox: %v", got)
	}
	if len(inbox(caller.ID)) != 0 {
		t.Error("a caller got the recap")
	}
	list, _ := srv.Netmail.Inbox(root.ID)
	body := list[0].Body
	for _, want := range []string{"Callers", "2 by 2 caller(s)", "telnet 1", "Right now", "-- NullModem BBS"} {
		if !strings.Contains(body, want) {
			t.Errorf("recap lacks %q:\n%s", want, body)
		}
	}
	for _, l := range strings.Split(body, "\n") {
		if len([]rune(l)) > 79 {
			t.Errorf("line over 79 columns: %q", l)
		}
	}
	if list[0].FromName != recapFrom {
		t.Errorf("from %q", list[0].FromName)
	}
}
