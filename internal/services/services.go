// Package services keeps track of the BBS's daemons (bbs, mailer, web)
// in the shared database, so the web admin can show which are running
// and ask one to restart -- without any access to Docker itself. A
// daemon asked to restart exits on its own, and its container's
// "restart: unless-stopped" policy starts it again.
package services

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
)

// The daemons.
const (
	BBS    = "bbs"
	Mailer = "mailer"
	Web    = "web"
)

// Names lists the daemons in display order.
var Names = []string{BBS, Mailer, Web}

// DoorPrefix starts the name of a door's background program (see
// internal/doors.Supervisor), listed after the daemons: "door:uMRC".
const DoorPrefix = "door:"

// Restart modes.
const (
	ModeNow  = "now"
	ModeIdle = "idle" // bbs only: once nobody is online
)

// Timing: a heartbeat every HeartbeatEvery; a daemon whose last one is
// older than StaleAfter counts as not running.
var (
	HeartbeatEvery = 5 * time.Second
	StaleAfter     = 20 * time.Second
	pollEvery      = 2 * time.Second
)

// Store reads and writes the services table.
type Store struct {
	db *sql.DB
}

// NewStore returns a Store on db.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func now() int64 { return time.Now().UnixMilli() }

// Status is one daemon as the web admin shows it.
type Status struct {
	Name               string
	Version            string
	PID                int
	StartedAt          time.Time
	HeartbeatAt        time.Time
	RestartRequestedAt time.Time
	RestartMode        string
	// RestartNeeded lists why a restart is due (empty when none is).
	RestartNeeded []string
}

// Running reports whether the daemon's heartbeat is recent.
func (s Status) Running() bool {
	return !s.HeartbeatAt.IsZero() && time.Since(s.HeartbeatAt) < StaleAfter
}

// RestartPending reports whether a restart was asked for and hasn't
// happened yet.
func (s Status) RestartPending() bool {
	return !s.RestartRequestedAt.IsZero() && s.RestartRequestedAt.After(s.StartedAt)
}

func fromMillis(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// List returns every known daemon, in Names order, with a zero Status
// for one that never registered.
func (st *Store) List() ([]Status, error) {
	rows, err := st.db.Query(`SELECT name, version, pid, started_at, heartbeat_at, restart_requested_at, restart_mode, restart_needed FROM services`)
	if err != nil {
		return nil, fmt.Errorf("services: listing: %w", err)
	}
	defer rows.Close()
	byName := map[string]Status{}
	for rows.Next() {
		var s Status
		var started, beat, requested int64
		var needed string
		if err := rows.Scan(&s.Name, &s.Version, &s.PID, &started, &beat, &requested, &s.RestartMode, &needed); err != nil {
			return nil, fmt.Errorf("services: listing: %w", err)
		}
		s.StartedAt, s.HeartbeatAt, s.RestartRequestedAt = fromMillis(started), fromMillis(beat), fromMillis(requested)
		if needed != "" {
			s.RestartNeeded = strings.Split(needed, "; ")
		}
		byName[s.Name] = s
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("services: listing: %w", err)
	}
	out := make([]Status, 0, len(byName))
	for _, n := range Names {
		s, ok := byName[n]
		if !ok {
			s = Status{Name: n}
		}
		out = append(out, s)
	}
	var extra []string
	for n := range byName {
		if strings.HasPrefix(n, DoorPrefix) {
			extra = append(extra, n)
		}
	}
	slices.Sort(extra)
	for _, n := range extra {
		out = append(out, byName[n])
	}
	return out, nil
}

// Known reports whether name is a daemon or a registered door program.
func (st *Store) Known(name string) (bool, error) {
	if slices.Contains(Names, name) {
		return true, nil
	}
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM services WHERE name = ?`, name).Scan(&n); err != nil {
		return false, fmt.Errorf("services: looking up %s: %w", name, err)
	}
	return n > 0, nil
}

// RequestRestart asks daemon name to restart in mode.
func (st *Store) RequestRestart(name, mode string) error {
	// Always later than the daemon's own start, even within the same
	// millisecond -- a request that isn't counts as already handled.
	_, err := st.db.Exec(`INSERT INTO services (name, restart_requested_at, restart_mode) VALUES (?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET restart_requested_at = MAX(excluded.restart_requested_at, services.started_at + 1),
			restart_mode = excluded.restart_mode`,
		name, now(), mode)
	if err != nil {
		return fmt.Errorf("services: requesting restart of %s: %w", name, err)
	}
	return nil
}

// MarkRestartNeeded records that daemon name needs a restart for
// reason (once, however often the same change is saved again).
func (st *Store) MarkRestartNeeded(name, reason string) error {
	var needed string
	err := st.db.QueryRow(`SELECT restart_needed FROM services WHERE name = ?`, name).Scan(&needed)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("services: marking %s: %w", name, err)
	}
	for _, r := range strings.Split(needed, "; ") {
		if r == reason {
			return nil
		}
	}
	if needed != "" {
		needed += "; "
	}
	needed += reason
	_, err = st.db.Exec(`INSERT INTO services (name, restart_needed) VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET restart_needed = excluded.restart_needed`, name, needed)
	if err != nil {
		return fmt.Errorf("services: marking %s: %w", name, err)
	}
	return nil
}

// RegisterProcess records a door's background program (name starting
// with DoorPrefix) as just started with pid; the caller keeps it fresh
// with Heartbeat and watches RestartRequested. It returns the start
// time to pass to RestartRequested.
func (st *Store) RegisterProcess(name, version string, pid int) (int64, error) {
	t := now()
	_, err := st.db.Exec(`INSERT INTO services (name, version, pid, started_at, heartbeat_at, restart_needed, restart_mode)
		VALUES (?, ?, ?, ?, ?, '', '')
		ON CONFLICT(name) DO UPDATE SET version = excluded.version, pid = excluded.pid, started_at = excluded.started_at,
			heartbeat_at = excluded.heartbeat_at, restart_needed = '', restart_mode = ''`,
		name, version, pid, t, t)
	if err != nil {
		return 0, fmt.Errorf("services: registering %s: %w", name, err)
	}
	return t, nil
}

// Heartbeat marks name as still running.
func (st *Store) Heartbeat(name string) error {
	_, err := st.db.Exec(`UPDATE services SET heartbeat_at = ? WHERE name = ?`, now(), name)
	return err
}

// RestartRequested reports whether a restart of name was asked for
// after startedAt.
func (st *Store) RestartRequested(name string, startedAt int64) bool {
	var requested int64
	err := st.db.QueryRow(`SELECT restart_requested_at FROM services WHERE name = ?`, name).Scan(&requested)
	return err == nil && requested > startedAt
}

// RemoveDoorPrograms deletes the rows of door programs not in keep --
// doors removed (or whose program was) while the bbs daemon was down.
func (st *Store) RemoveDoorPrograms(keep []string) error {
	rows, err := st.db.Query(`SELECT name FROM services WHERE name LIKE ?`, DoorPrefix+"%")
	if err != nil {
		return fmt.Errorf("services: listing door programs: %w", err)
	}
	var drop []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err == nil && !slices.Contains(keep, n) {
			drop = append(drop, n)
		}
	}
	rows.Close()
	for _, n := range drop {
		if _, err := st.db.Exec(`DELETE FROM services WHERE name = ?`, n); err != nil {
			return fmt.Errorf("services: removing %s: %w", n, err)
		}
	}
	return nil
}

// Instance is one running daemon's registration.
type Instance struct {
	st        *Store
	name      string
	startedAt int64
}

// Register records daemon name as just started (clearing any restart
// it was waiting for) and returns its registration; call Run next.
func (st *Store) Register(name, version string) (*Instance, error) {
	t := now()
	_, err := st.db.Exec(`INSERT INTO services (name, version, pid, started_at, heartbeat_at, restart_needed, restart_mode)
		VALUES (?, ?, ?, ?, ?, '', '')
		ON CONFLICT(name) DO UPDATE SET version = excluded.version, pid = excluded.pid, started_at = excluded.started_at,
			heartbeat_at = excluded.heartbeat_at, restart_needed = '', restart_mode = ''`,
		name, version, os.Getpid(), t, t)
	if err != nil {
		return nil, fmt.Errorf("services: registering %s: %w", name, err)
	}
	return &Instance{st: st, name: name, startedAt: t}, nil
}

// Run keeps the heartbeat fresh until ctx ends, and calls onRestart
// (once) with the mode when a restart is requested -- onRestart
// decides when to actually exit.
func (in *Instance) Run(ctx context.Context, onRestart func(mode string)) {
	poll := time.NewTicker(pollEvery)
	defer poll.Stop()
	lastBeat := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-poll.C:
		}
		if time.Since(lastBeat) >= HeartbeatEvery {
			in.st.db.Exec(`UPDATE services SET heartbeat_at = ? WHERE name = ?`, now(), in.name)
			lastBeat = time.Now()
		}
		var requested int64
		var mode string
		err := in.st.db.QueryRow(`SELECT restart_requested_at, restart_mode FROM services WHERE name = ?`, in.name).Scan(&requested, &mode)
		if err == nil && requested > in.startedAt {
			onRestart(mode)
			return
		}
	}
}
