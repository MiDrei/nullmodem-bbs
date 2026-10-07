package doors

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/db"
)

type quietLog struct{ warns []string }

func (l *quietLog) Info(string, ...any)     {}
func (l *quietLog) Warn(f string, a ...any) { l.warns = append(l.warns, f) }

func TestDailyDue(t *testing.T) {
	d := Door{Name: "TW", Daily: "TWMAINT", DailyAt: "00:05"}
	day := time.Date(2026, 10, 2, 0, 4, 0, 0, time.Local)
	if dailyDue(d, DailyState{}, day) {
		t.Error("due before its time")
	}
	at := day.Add(2 * time.Minute)
	if !dailyDue(d, DailyState{}, at) {
		t.Error("not due after its time, never run")
	}
	if dailyDue(d, DailyState{LastAt: at}, at.Add(time.Hour)) {
		t.Error("due twice the same day")
	}
	if !dailyDue(d, DailyState{LastAt: at, RequestedAt: at.Add(time.Minute)}, at.Add(time.Hour)) {
		t.Error("Run now not honoured")
	}
	if !dailyDue(d, DailyState{LastAt: at}, at.Add(24*time.Hour)) {
		t.Error("not due the next day")
	}
}

func TestSchedulerRunsOnceAndWaitsWhileBusy(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	runs := 0
	busy := true
	s := &Scheduler{DB: sqlDB, Logger: &quietLog{},
		Doors: func() []Door {
			return []Door{{Name: "TW", Kind: "dosbox", Daily: "TWMAINT", DailyAt: "00:05"}, {Name: "LORD"}}
		},
		RunOne: func(context.Context, Door) (string, error) {
			if busy {
				return "", ErrBusy
			}
			runs++
			return "ok", nil
		}}
	now := time.Date(2026, 10, 2, 1, 0, 0, 0, time.Local)
	s.Tick(context.Background(), now)
	if runs != 0 {
		t.Fatal("ran while busy")
	}
	busy = false
	s.Tick(context.Background(), now.Add(time.Minute))
	s.Tick(context.Background(), now.Add(2*time.Minute))
	if runs != 1 {
		t.Fatalf("%d runs, want 1", runs)
	}
	states, _ := DailyStates(sqlDB)
	if st := states["TW"]; !st.OK || st.Detail != "ok" {
		t.Fatalf("state %+v", st)
	}
	RequestDaily(sqlDB, "TW")
	s.Tick(context.Background(), now.Add(3*time.Minute))
	if runs != 2 {
		t.Fatal("Run now didn't run")
	}
	s.Tick(context.Background(), now.Add(4*time.Minute))
	if runs != 2 {
		t.Fatal("the request wasn't cleared")
	}
}

func TestRunDailyNative(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "maint.sh")
	os.WriteFile(script, []byte("#!/bin/sh\necho \"new day: $1\"\n"), 0o755)
	out, err := RunDaily(context.Background(), Door{Name: "N", Dir: dir, Daily: "maint.sh turns"})
	if err != nil || !strings.Contains(out, "new day: turns") {
		t.Fatalf("%q %v", out, err)
	}
	// Busy: someone is in the door.
	release, _ := acquire(Door{Name: "N"})
	defer release()
	if _, err := RunDaily(context.Background(), Door{Name: "N", Dir: dir, Daily: "maint.sh"}); err != ErrBusy {
		t.Fatalf("while played: %v", err)
	}
}
