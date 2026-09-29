package services

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/db"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return NewStore(sqlDB)
}

func TestListShowsEveryDaemonAndRegistrationClearsNeeds(t *testing.T) {
	st := newStore(t)
	if err := st.MarkRestartNeeded(Mailer, "BinkP settings changed"); err != nil {
		t.Fatal(err)
	}
	st.MarkRestartNeeded(Mailer, "BinkP settings changed") // once only
	st.MarkRestartNeeded(Mailer, "Networks changed")

	list, err := st.List()
	if err != nil || len(list) != 3 || list[0].Name != BBS || list[1].Name != Mailer {
		t.Fatalf("List = %+v, %v", list, err)
	}
	if got := list[1].RestartNeeded; len(got) != 2 || got[0] != "BinkP settings changed" {
		t.Fatalf("RestartNeeded = %q", got)
	}
	if list[0].Running() {
		t.Fatal("a never-registered daemon counts as running")
	}

	if _, err := st.Register(Mailer, "v1"); err != nil {
		t.Fatal(err)
	}
	list, _ = st.List()
	if !list[1].Running() || list[1].Version != "v1" || len(list[1].RestartNeeded) != 0 {
		t.Fatalf("after Register: %+v", list[1])
	}
}

func TestRunReportsARestartRequestedAfterStart(t *testing.T) {
	old := pollEvery
	pollEvery = 20 * time.Millisecond
	defer func() { pollEvery = old }()

	st := newStore(t)
	// A request from before this start must not trigger a restart.
	st.RequestRestart(BBS, ModeNow)
	time.Sleep(5 * time.Millisecond)
	in, err := st.Register(BBS, "v1")
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go in.Run(ctx, func(mode string) { got <- mode })

	select {
	case m := <-got:
		t.Fatalf("restarted on a request older than its start (mode %q)", m)
	case <-time.After(150 * time.Millisecond):
	}
	list, _ := st.List()
	if list[0].RestartPending() {
		t.Fatal("an old request counts as pending")
	}

	time.Sleep(5 * time.Millisecond)
	st.RequestRestart(BBS, ModeIdle)
	select {
	case m := <-got:
		if m != ModeIdle {
			t.Fatalf("mode = %q, want idle", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("restart request not noticed")
	}
}
