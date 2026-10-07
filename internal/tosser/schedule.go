package tosser

import (
	"database/sql"
	"fmt"
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
