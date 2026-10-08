package tosser

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/config"
)

// UplinkPollStore persists when each configured BinkP uplink was last
// polled, so a per-uplink poll interval (see IsDue) survives
// cmd/mailer restarts -- some hubs only permit polling every hour or
// two and reject a caller connecting more often, so forgetting how
// recently we tried on every restart would risk violating that limit.
type UplinkPollStore struct {
	db *sql.DB
}

// NewUplinkPollStore wraps an already-open database connection.
func NewUplinkPollStore(db *sql.DB) *UplinkPollStore { return &UplinkPollStore{db: db} }

// LastPolledAt returns when host was last polled (an attempt is
// recorded via RecordAttempt regardless of whether it succeeded), or
// the zero Time if it never has been.
func (s *UplinkPollStore) LastPolledAt(host string) (time.Time, error) {
	var t time.Time
	err := s.db.QueryRow(`SELECT last_polled_at FROM binkp_uplink_polls WHERE host = ?`, host).Scan(&t)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("tosser: loading last poll time for %s: %w", host, err)
	}
	return t, nil
}

// RecordAttempt records that host was just polled -- called before
// dialing, not after, so an overlapping check on the next tick sees
// the attempt immediately rather than potentially double-dialing
// while the first attempt is still in flight. Recording regardless of
// outcome (not just on success) matters for the same reason IsDue
// exists at all: a rejected attempt (e.g. the uplink's own "polling
// too frequently" busy response) still counts as contact for rate-
// limiting purposes.
func (s *UplinkPollStore) RecordAttempt(host string) error {
	if _, err := s.db.Exec(
		`INSERT INTO binkp_uplink_polls (host, last_polled_at) VALUES (?, CURRENT_TIMESTAMP)
		 ON CONFLICT(host) DO UPDATE SET last_polled_at = excluded.last_polled_at`,
		host,
	); err != nil {
		return fmt.Errorf("tosser: recording poll attempt for %s: %w", host, err)
	}
	return nil
}

// IsDue reports whether uplink should be polled now, given when it
// was last polled (the zero Time if never) and now. defaultInterval
// is the fallback for an uplink with no PollIntervalSeconds of its
// own (config.Config's Binkp.PollIntervalSeconds).
func IsDue(uplink config.BinkpUplink, lastPolled, now time.Time, defaultInterval time.Duration) bool {
	if lastPolled.IsZero() {
		return true
	}
	interval := defaultInterval
	if uplink.PollIntervalSeconds > 0 {
		interval = time.Duration(uplink.PollIntervalSeconds) * time.Second
	}
	return now.Sub(lastPolled) >= interval
}

// DialBackoff spaces out crash dials (see cmd/mailer's
// dialedForPendingMail) to an uplink whose last ones failed: each
// failure in a row doubles the wait, from FirstWait up to MaxWait,
// and a session that works starts it over. Without it, mail waiting
// for an unreachable hub was dialed every few minutes for hours --
// noise in the log, and a cadence that gets a caller blocked by the
// hub's own rate limiting. Kept in memory: a restart tries again.
type DialBackoff struct {
	FirstWait, MaxWait time.Duration

	mu    sync.Mutex
	hosts map[string]dialFailures
}

type dialFailures struct {
	count int
	last  time.Time
}

// Wait is how long after its last failure host is left alone; zero
// when its last dial worked.
func (b *DialBackoff) Wait(host string) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.wait(b.hosts[host].count)
}

func (b *DialBackoff) wait(count int) time.Duration {
	if count == 0 {
		return 0
	}
	w := b.FirstWait
	for i := 1; i < count && w < b.MaxWait; i++ {
		w *= 2
	}
	return min(w, b.MaxWait)
}

// Ready reports whether host may be dialed at now.
func (b *DialBackoff) Ready(host string, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	f := b.hosts[host]
	return f.count == 0 || now.Sub(f.last) >= b.wait(f.count)
}

// Failed records a failed dial to host at now and returns how many
// failed in a row.
func (b *DialBackoff) Failed(host string, now time.Time) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.hosts == nil {
		b.hosts = map[string]dialFailures{}
	}
	f := b.hosts[host]
	f.count++
	f.last = now
	b.hosts[host] = f
	return f.count
}

// Worked records a session with host that worked.
func (b *DialBackoff) Worked(host string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.hosts, host)
}
