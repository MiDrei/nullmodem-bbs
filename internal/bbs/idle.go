package bbs

import (
	"context"
	"time"
)

// LoginIdleLimit is how long a caller may sit at the login without
// typing before the BBS hangs up.
var LoginIdleLimit = 3 * time.Minute

// idleCheckEvery is how often watchIdle looks.
var idleCheckEvery = 15 * time.Second

// touch records input from the caller.
func (t *Terminal) touch() { t.lastInput.Store(time.Now().UnixNano()) }

// Busy pauses the idle watch while the connection is handed to
// something that reads it itself (a door, a file transfer, RLogin);
// the returned func ends the pause.
func (t *Terminal) Busy() (done func()) {
	t.busy.Add(1)
	return func() {
		t.touch()
		t.busy.Add(-1)
	}
}

// idleFor is how long the caller hasn't typed, 0 while Busy.
func (t *Terminal) idleFor(now time.Time) time.Duration {
	if t.busy.Load() > 0 {
		return 0
	}
	return now.Sub(time.Unix(0, t.lastInput.Load()))
}

// watchIdle hangs up (onIdle) once the caller hasn't typed for
// limit() -- 0 or less never -- and returns then or when ctx ends.
// Without it a silent connection held its node for good.
func (t *Terminal) watchIdle(ctx context.Context, limit func() time.Duration, onIdle func()) {
	if t.lastInput.Load() == 0 {
		t.touch()
	}
	tick := time.NewTicker(idleCheckEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			if l := limit(); l > 0 && t.idleFor(now) > l {
				onIdle()
				return
			}
		}
	}
}
