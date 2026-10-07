package bbs

import (
	"context"
	"testing"
	"time"
)

func TestWatchIdleHangsUpAndPausesWhileBusy(t *testing.T) {
	old := idleCheckEvery
	idleCheckEvery = 5 * time.Millisecond
	defer func() { idleCheckEvery = old }()

	term := &Terminal{}
	hung := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := term.Busy()
	go term.watchIdle(ctx, func() time.Duration { return 30 * time.Millisecond }, func() { close(hung) })
	select {
	case <-hung:
		t.Fatal("hung up while busy (in a door)")
	case <-time.After(100 * time.Millisecond):
	}
	done()
	select {
	case <-hung:
	case <-time.After(time.Second):
		t.Fatal("never hung up an idle caller")
	}
}

func TestWatchIdleKeepsATypingCaller(t *testing.T) {
	old := idleCheckEvery
	idleCheckEvery = 5 * time.Millisecond
	defer func() { idleCheckEvery = old }()

	term := &Terminal{}
	hung := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go term.watchIdle(ctx, func() time.Duration { return 40 * time.Millisecond }, func() { close(hung) })
	for i := 0; i < 10; i++ {
		term.touch()
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-hung:
		t.Fatal("hung up on a caller who kept typing")
	default:
	}
}
