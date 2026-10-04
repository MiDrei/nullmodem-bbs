// Package health watches that the BBS keeps working and tells the
// sysop when it doesn't -- on their phone (a push) and on the admin
// dashboard -- once when a problem starts and once when it's over:
// a daemon or a door's background program that stopped, an uplink not
// reached for two days, a backup that didn't happen, a disk filling
// up, netmail that doesn't go out.
package health

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/backup"
	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/i18n"
	"git.maik.ch/nullmodem/bbs/internal/offsite"
	"git.maik.ch/nullmodem/bbs/internal/services"
)

// Problem is something not working.
type Problem struct {
	Key    string    `json:"key"`
	Title  string    `json:"title"`
	Detail string    `json:"detail"`
	Since  time.Time `json:"since"`
}

// Limits of the checks.
var (
	ServiceStale  = 2 * time.Minute
	UplinkSilence = 48 * time.Hour
	BackupAge     = 26 * time.Hour
	NetmailStuck  = 48 * time.Hour
	DiskMinFree   = uint64(1 << 30)
	DiskMinShare  = 0.05
)

// Env is what the checks look at.
type Env struct {
	DB     *sql.DB
	Config func() *config.Config
	// Self is the daemon running the checks (not checked itself).
	Self string
	// StartedAt is when it started: no backup is expected before a
	// day has passed.
	StartedAt time.Time
	// Disk returns free and total bytes where dir is; nil skips.
	Disk func(dir string) (free, total uint64)
	Now  func() time.Time
	// Extra adds problems only the running daemon knows (the Discord
	// bridge); nil adds none.
	Extra func() []Problem
}

func (e Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// Check returns the problems now.
func Check(ctx context.Context, e Env) ([]Problem, error) {
	var out []Problem
	add := func(key, title, detail string) {
		out = append(out, Problem{Key: key, Title: title, Detail: detail})
	}
	now := e.now()
	cfg := e.Config()

	// Daemons and doors' background programs.
	list, err := services.NewStore(e.DB).List()
	if err != nil {
		return nil, err
	}
	for _, st := range list {
		if st.Name == e.Self || st.HeartbeatAt.IsZero() {
			continue
		}
		if age := now.Sub(st.HeartbeatAt); age > ServiceStale {
			title := i18n.Ref("health.service_down", "NAME", st.Name)
			if d, ok := strings.CutPrefix(st.Name, services.DoorPrefix); ok {
				title = i18n.Ref("health.door_program_down", "DOOR", d)
			}
			add("service:"+st.Name, title, i18n.Ref("health.no_sign", "AGE", age.Round(time.Minute)))
		}
	}

	// Uplinks: a successful session, either way, within UplinkSilence.
	for _, u := range cfg.Binkp.Uplinks {
		if u.Downlink || u.Address == "" {
			continue
		}
		var last sql.NullString
		if err := e.DB.QueryRowContext(ctx, `SELECT MAX(started_at) FROM binkp_sessions WHERE peer_address = ? AND outcome = 'ok'`,
			u.Address).Scan(&last); err != nil {
			return nil, err
		}
		cutoff := now.Add(-UplinkSilence).UTC().Format("2006-01-02 15:04:05")
		if last.Valid && last.String[:min(19, len(last.String))] >= cutoff {
			continue
		}
		if u.PollDisabled {
			// Crash only: called just when there's mail, so silence is
			// normal -- a problem only when the last try failed.
			var lastOutcome string
			e.DB.QueryRowContext(ctx, `SELECT outcome FROM binkp_sessions WHERE peer_address = ? ORDER BY id DESC LIMIT 1`,
				u.Address).Scan(&lastOutcome)
			if lastOutcome == "" || lastOutcome == "ok" {
				continue
			}
		}
		var detail string
		e.DB.QueryRowContext(ctx, `SELECT detail FROM binkp_sessions WHERE peer_address = ? AND outcome <> 'ok' ORDER BY id DESC LIMIT 1`,
			u.Address).Scan(&detail)
		title := i18n.Ref("health.uplink_silent", "ADDRESS", u.Address, "HOST", u.Host)
		if !last.Valid {
			title = i18n.Ref("health.uplink_never", "ADDRESS", u.Address, "HOST", u.Host)
		}
		if detail != "" {
			detail = i18n.Ref("health.last_error", "ERROR", detail)
		}
		add("uplink:"+u.Address, title, detail)
	}

	// The nightly backup.
	if b := cfg.Backup; b.On() {
		list, err := backup.List(b.Directory())
		if err == nil {
			switch {
			case len(list) > 0 && now.Sub(list[0].Time) > BackupAge:
				add("backup", i18n.Ref("health.backup_overdue"), i18n.Ref("health.backup_newest", "WHEN", list[0].Time.Format("02.01. 15:04")))
			case len(list) == 0 && now.Sub(e.StartedAt) > BackupAge:
				add("backup", i18n.Ref("health.no_backup"), i18n.Ref("health.no_backup_detail"))
			}
		}
		if e.Disk != nil {
			if free, total := e.Disk(b.Directory()); total > 0 && (free < DiskMinFree || float64(free) < DiskMinShare*float64(total)) {
				add("disk", i18n.Ref("health.disk_full"), i18n.Ref("health.disk_free", "FREE", fmt.Sprintf("%.1f", float64(free)/(1<<30)), "TOTAL", fmt.Sprintf("%.0f", float64(total)/(1<<30))))
			}
		}
	}

	// The off-site copy of the backups.
	if o := cfg.Backup.Offsite; o.Enabled {
		st := offsite.LoadStatus(e.DB)
		switch {
		case st.LastError != "":
			add("offsite", i18n.Ref("health.offsite_failed"), st.LastError)
		case !st.LastOK.IsZero() && now.Sub(st.LastOK) > 2*BackupAge:
			add("offsite", i18n.Ref("health.offsite_overdue"), i18n.Ref("health.offsite_last", "WHEN", st.LastOK.Format("02.01. 15:04")))
		}
	}

	// Doors' daily maintenance that failed (the last run).
	rows, err := e.DB.QueryContext(ctx, `SELECT door, detail FROM door_daily WHERE last_at > 0 AND ok = 0`)
	if err == nil {
		for rows.Next() {
			var door, detail string
			rows.Scan(&door, &detail)
			if i := strings.IndexByte(detail, '\n'); i > 0 {
				detail = detail[:i]
			}
			add("door-daily:"+door, i18n.Ref("health.door_daily_failed", "DOOR", door), detail)
		}
		rows.Close()
	}

	// Netmail waiting to go out.
	var stuck int
	cutoff := now.Add(-NetmailStuck).UTC().Format("2006-01-02 15:04:05")
	if err := e.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM netmail_messages
		WHERE to_address <> '' AND to_user_id IS NULL AND sent_at IS NULL AND substr(posted_at, 1, 19) < ?`, cutoff).Scan(&stuck); err != nil {
		return nil, err
	}
	if stuck > 0 {
		add("netmail", i18n.Ref("health.netmail_stuck", "COUNT", stuck), i18n.Ref("health.netmail_stuck_detail"))
	}
	if e.Extra != nil {
		out = append(out, e.Extra()...)
	}
	return out, nil
}

// Current returns the problems recorded by the last Track.
func Current(db *sql.DB) ([]Problem, error) {
	rows, err := db.Query(`SELECT key, title, detail, since FROM health_problems ORDER BY since`)
	if err != nil {
		return nil, fmt.Errorf("health: %w", err)
	}
	defer rows.Close()
	out := []Problem{}
	for rows.Next() {
		var p Problem
		var since int64
		if err := rows.Scan(&p.Key, &p.Title, &p.Detail, &since); err != nil {
			return nil, fmt.Errorf("health: %w", err)
		}
		p.Since = time.UnixMilli(since)
		out = append(out, p)
	}
	return out, rows.Err()
}

// Track records what Check found and returns the problems that just
// started and those just over.
func Track(db *sql.DB, found []Problem, now time.Time) (started, over []Problem, err error) {
	prev, err := Current(db)
	if err != nil {
		return nil, nil, err
	}
	was := map[string]Problem{}
	for _, p := range prev {
		was[p.Key] = p
	}
	is := map[string]bool{}
	for _, p := range found {
		is[p.Key] = true
		if _, ok := was[p.Key]; ok {
			// Still there: the detail may have changed.
			db.Exec(`UPDATE health_problems SET title = ?, detail = ? WHERE key = ?`, p.Title, p.Detail, p.Key)
			continue
		}
		p.Since = now
		if _, err := db.Exec(`INSERT INTO health_problems (key, title, detail, since) VALUES (?, ?, ?, ?)`,
			p.Key, p.Title, p.Detail, now.UnixMilli()); err != nil {
			return nil, nil, fmt.Errorf("health: %w", err)
		}
		started = append(started, p)
	}
	for key, p := range was {
		if is[key] {
			continue
		}
		if _, err := db.Exec(`DELETE FROM health_problems WHERE key = ?`, key); err != nil {
			return nil, nil, fmt.Errorf("health: %w", err)
		}
		over = append(over, p)
	}
	sort.Slice(over, func(i, j int) bool { return over[i].Key < over[j].Key })
	return started, over, nil
}

// Logger is what the monitor reports to.
type Logger interface {
	Info(format string, args ...any)
	Warn(format string, args ...any)
}

// Monitor checks every few minutes and calls notify for problems that
// start (ok false) and end (ok true).
func Monitor(ctx context.Context, e Env, every time.Duration, log Logger, notify func(p Problem, ok bool)) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		found, err := Check(ctx, e)
		if err != nil {
			log.Warn("health check: %v", err)
			continue
		}
		started, over, err := Track(e.DB, found, e.now())
		if err != nil {
			log.Warn("health check: %v", err)
			continue
		}
		for _, p := range started {
			log.Warn("problem: %s%s", p.Title, suffix(p.Detail))
			notify(p, false)
		}
		for _, p := range over {
			log.Info("problem over: %s", p.Title)
			notify(p, true)
		}
	}
}

func suffix(detail string) string {
	if detail == "" {
		return ""
	}
	return " (" + detail + ")"
}
