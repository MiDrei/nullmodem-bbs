// Package maintenance is the nightly cleanup: old echomail, old files,
// read netmail, the log, BinkP transcripts and the inbound archive,
// then compacting the database -- by the limits in config.Maintenance
// and each area's own. The mailer runs it at night; the web admin can
// preview it (nothing deleted, only counted) or run it at once.
//
// Never deleted, whatever the limits: a local post not yet handed to
// the hub, netmail not yet sent or not yet read, and any message
// younger than MinAge (a count limit included).
package maintenance

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/applog"
	"github.com/midrei/nullmodem-bbs/internal/archive"
	"github.com/midrei/nullmodem-bbs/internal/binkplog"
	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/file"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

// MinAge protects every message younger than this -- as long as a
// point (reader app) gets on first subscribing (internal/tosser's
// pointBacklog), so nothing goes before a point could fetch it.
const MinAge = 14 * 24 * time.Hour

// Deps is what a run works on.
type Deps struct {
	DB         *sql.DB
	Files      *file.Store
	Logs       *applog.Store
	SessionLog *binkplog.Store
	Archive    *archive.Store
	// DBPath, for the size before and after compacting.
	DBPath string
}

// AreaCount is how much went from one area.
type AreaCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// Report is what a run deleted -- or, with DryRun, would delete.
type Report struct {
	DryRun       bool        `json:"dry_run"`
	StartedAt    time.Time   `json:"started_at"`
	Seconds      float64     `json:"seconds"`
	Messages     int         `json:"messages"`
	MessageAreas []AreaCount `json:"message_areas"`
	Files        int         `json:"files"`
	FileBytes    int64       `json:"file_bytes"`
	Netmail      int         `json:"netmail"`
	Logs         int         `json:"logs"`
	Transcripts  int         `json:"transcripts"`
	Archive      int         `json:"archive"`
	// PendingUsers are accounts deleted for waiting too long for
	// approval.
	PendingUsers int `json:"pending_users"`
	// DBBytesBefore and DBBytesAfter are the database file's size,
	// WALBytes the write-ahead log's beside it after the run.
	DBBytesBefore int64 `json:"db_bytes_before"`
	DBBytesAfter  int64 `json:"db_bytes_after"`
	WALBytes      int64 `json:"wal_bytes"`
	// Vacuumed is whether the database was compacted: only when enough
	// of it is unused (see vacuumShare).
	Vacuumed bool     `json:"vacuumed"`
	Errors   []string `json:"errors"`
}

// sqlTime is how a time is compared against the stored TEXT
// timestamps: their first 19 characters ("2006-01-02 15:04:05") in
// UTC, whatever follows them -- CURRENT_TIMESTAMP and Go-written
// times differ only after that.
func sqlTime(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05") }

// Run cleans up by cfg, or with dry only counts what it would delete.
// It keeps going past a failing step and lists the failure in the
// report.
func Run(ctx context.Context, d Deps, cfg config.MaintenanceConfig, dry bool) Report {
	now := time.Now()
	r := Report{DryRun: dry, StartedAt: now, MessageAreas: []AreaCount{}, Errors: []string{}}
	fail := func(step string, err error) {
		if err != nil {
			r.Errors = append(r.Errors, step+": "+err.Error())
		}
	}
	r.DBBytesBefore = fileSize(d.DBPath)

	fail("messages", messages(ctx, d.DB, cfg, now, dry, &r))
	fail("files", files(d, cfg, now, dry, &r))
	fail("netmail", readNetmail(d.DB, cfg, now, dry, &r))
	if d.Logs != nil {
		n, err := d.Logs.Trim(cfg.LogRows(), dry)
		r.Logs = n
		fail("log", err)
	}
	if d.SessionLog != nil {
		n, err := d.SessionLog.PruneOlderThan(now, days(cfg.TranscriptDays()), dry)
		r.Transcripts = n
		fail("transcripts", err)
	}
	if d.Archive != nil {
		n, err := d.Archive.PruneOlderThan(now, days(cfg.ArchiveDays()), dry)
		r.Archive = n
		fail("inbound archive", err)
	}
	if keep := cfg.PendingDays(); keep > 0 {
		n, err := pendingUsers(d.DB, now.Add(-days(keep)), dry)
		r.PendingUsers = n
		fail("unapproved accounts", err)
	}
	if !dry && cfg.VacuumAfter() {
		vacuumed, err := compact(ctx, d.DB)
		r.Vacuumed = vacuumed
		fail("compacting the database", err)
	}
	r.DBBytesAfter = fileSize(d.DBPath)
	r.WALBytes = fileSize(walPath(d.DBPath))
	r.Seconds = time.Since(now).Seconds()
	return r
}

func days(n int) time.Duration { return time.Duration(n) * 24 * time.Hour }

func fileSize(path string) int64 {
	if path == "" {
		return 0
	}
	if fi, err := os.Stat(path); err == nil {
		return fi.Size()
	}
	return 0
}

func walPath(path string) string {
	if path == "" {
		return ""
	}
	return path + "-wal"
}

// vacuumShare is how much of the database must be unused pages before
// it is compacted: a VACUUM rewrites the whole database (through the
// WAL, which grows to its size), not worth it for the little a nightly
// run frees -- SQLite reuses free pages anyway.
const vacuumShare = 0.10

// Checkpoint retries: the other daemons' readers can keep the WAL from
// being emptied for a moment.
var (
	checkpointTries = 30
	checkpointPause = 2 * time.Second
)

// compact vacuums the database if enough of it is unused, then empties
// the WAL back into it and truncates it.
func compact(ctx context.Context, db *sql.DB) (vacuumed bool, err error) {
	var pages, free int64
	if err := db.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pages); err != nil {
		return false, err
	}
	if err := db.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&free); err != nil {
		return false, err
	}
	if pages > 0 && float64(free) >= vacuumShare*float64(pages) {
		if _, err := db.ExecContext(ctx, `VACUUM`); err != nil {
			return false, err
		}
		vacuumed = true
	}
	for i := 0; ; i++ {
		var busy, logPages, done int64
		if err := db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logPages, &done); err != nil {
			return vacuumed, err
		}
		if busy == 0 {
			return vacuumed, nil
		}
		if i+1 >= checkpointTries {
			return vacuumed, fmt.Errorf("the WAL could not be emptied: the database stayed busy")
		}
		select {
		case <-ctx.Done():
			return vacuumed, ctx.Err()
		case <-time.After(checkpointPause):
		}
	}
}

// deletable is every message a limit may take: not a local post still
// waiting for the hub, and not younger than MinAge.
const deletable = `NOT (from_user_id IS NOT NULL AND sent_at IS NULL) AND substr(posted_at, 1, 19) < ?`

// messages applies each area's limits: its own (keep_days/keep_max:
// -1 keep, 0 default), else a data area's or the general default.
func messages(ctx context.Context, db *sql.DB, cfg config.MaintenanceConfig, now time.Time, dry bool, r *Report) error {
	rows, err := db.QueryContext(ctx, `SELECT id, tag, hidden, keep_days, keep_max FROM message_areas ORDER BY tag`)
	if err != nil {
		return err
	}
	type area struct {
		id             int64
		tag            string
		hidden         bool
		keepDays, keep int
	}
	var areas []area
	for rows.Next() {
		var a area
		if err := rows.Scan(&a.id, &a.tag, &a.hidden, &a.keepDays, &a.keep); err != nil {
			rows.Close()
			return err
		}
		areas = append(areas, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	protect := sqlTime(now.Add(-MinAge))
	for _, a := range areas {
		keepDays := a.keepDays
		if keepDays == 0 {
			keepDays = cfg.MessageDays()
			if a.hidden {
				keepDays = cfg.DataAreaDays()
			}
		}
		keepMax := a.keep
		if keepMax == 0 {
			keepMax = cfg.MessageMax()
		}
		var conds []string
		var args []any
		if keepDays > 0 {
			conds = append(conds, `substr(posted_at, 1, 19) < ?`)
			args = append(args, sqlTime(now.Add(-days(keepDays))))
		}
		if keepMax > 0 {
			conds = append(conds, `id NOT IN (SELECT id FROM messages WHERE area_id = ? ORDER BY id DESC LIMIT ?)`)
			args = append(args, a.id, keepMax)
		}
		if len(conds) == 0 {
			continue
		}
		where := `area_id = ? AND ` + deletable + ` AND (` + joinOr(conds) + `)`
		args = append([]any{a.id, protect}, args...)
		var n int64
		if dry {
			err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE `+where, args...).Scan(&n)
		} else {
			var res sql.Result
			if res, err = db.ExecContext(ctx, `DELETE FROM messages WHERE `+where, args...); err == nil {
				n, err = res.RowsAffected()
			}
		}
		if err != nil {
			return fmt.Errorf("area %s: %w", a.tag, err)
		}
		if n > 0 {
			r.Messages += int(n)
			r.MessageAreas = append(r.MessageAreas, AreaCount{Tag: a.tag, Count: int(n)})
		}
	}
	return nil
}

func joinOr(conds []string) string {
	out := conds[0]
	for _, c := range conds[1:] {
		out += " OR " + c
	}
	return out
}

// files deletes files older than their area's limit (its own, else the
// default), from the disk too.
func files(d Deps, cfg config.MaintenanceConfig, now time.Time, dry bool, r *Report) error {
	if d.Files == nil {
		return nil
	}
	areas, err := d.Files.AllAreas()
	if err != nil {
		return err
	}
	pending, err := d.Files.PendingAreas()
	if err != nil {
		return err
	}
	for _, a := range append(areas, pending...) {
		keep := a.KeepDays
		if keep == 0 {
			keep = cfg.FileDays()
		}
		if keep <= 0 {
			continue
		}
		list, err := d.Files.ListFiles(a.ID)
		if err != nil {
			return err
		}
		for _, f := range list {
			if now.Sub(f.UploadedAt) <= days(keep) {
				continue
			}
			if !dry {
				if err := d.Files.DeleteFile(f.ID); err != nil {
					return fmt.Errorf("%s/%s: %w", a.Tag, f.Filename, err)
				}
			}
			r.Files++
			r.FileBytes += f.SizeBytes
		}
	}
	return nil
}

// readNetmail deletes read netmail older than the limit -- never unread,
// never still waiting to be sent.
func readNetmail(db *sql.DB, cfg config.MaintenanceConfig, now time.Time, dry bool, r *Report) error {
	keep := cfg.NetmailDays()
	if keep <= 0 {
		return nil
	}
	where := `read_at IS NOT NULL AND substr(posted_at, 1, 19) < ?
		AND NOT (to_user_id IS NULL AND to_address != '' AND sent_at IS NULL)`
	cutoff := sqlTime(now.Add(-days(keep)))
	var n int64
	var err error
	if dry {
		err = db.QueryRow(`SELECT COUNT(*) FROM netmail_messages WHERE `+where, cutoff).Scan(&n)
	} else {
		var res sql.Result
		if res, err = db.Exec(`DELETE FROM netmail_messages WHERE `+where, cutoff); err == nil {
			n, err = res.RowsAffected()
		}
	}
	r.Netmail = int(n)
	return err
}

// Save records a finished run (not a preview) for the web admin.
func Save(db *sql.DB, r Report) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO maintenance_runs (ran_at, report) VALUES (?, ?)`, r.StartedAt.UTC().UnixMilli(), string(b))
	return err
}

// Last is the newest recorded run, if any.
func Last(db *sql.DB) (*Report, error) {
	var raw string
	err := db.QueryRow(`SELECT report FROM maintenance_runs ORDER BY id DESC LIMIT 1`).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r Report
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// RanOn reports whether a run was recorded on day (the server's
// calendar day) -- so the nightly one runs once.
func RanOn(db *sql.DB, day time.Time) (bool, error) {
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM maintenance_runs WHERE ran_at >= ? AND ran_at < ?`,
		start.UnixMilli(), start.AddDate(0, 0, 1).UnixMilli()).Scan(&n)
	return n > 0, err
}

// ApplyLimits sets the limits the daemons also keep on their own as
// they go (log rows, transcripts and archive pruned after each
// session) to the configured ones.
func ApplyLimits(cfg config.MaintenanceConfig) {
	applog.SetMaxRows(cfg.LogRows())
	if d := cfg.TranscriptDays(); d > 0 {
		binkplog.RetentionPeriod = days(d)
	}
	if d := cfg.ArchiveDays(); d > 0 {
		archive.RetentionPeriod = days(d)
	}
}

// Logger is what the nightly run reports to.
type Logger interface {
	Info(format string, args ...any)
	Warn(format string, args ...any)
}

// Summary is a report in one line, for the log.
func (r Report) Summary() string {
	s := fmt.Sprintf("%d message(s), %d file(s) (%.1f MB), %d netmail, %d log entries, %d transcript(s), %d archived file(s), %d unapproved account(s)",
		r.Messages, r.Files, float64(r.FileBytes)/(1<<20), r.Netmail, r.Logs, r.Transcripts, r.Archive, r.PendingUsers)
	if r.DBBytesBefore > 0 {
		s += fmt.Sprintf("; database %.1f -> %.1f MB", float64(r.DBBytesBefore)/(1<<20), float64(r.DBBytesAfter)/(1<<20))
		if r.Vacuumed {
			s += " (compacted)"
		}
		s += fmt.Sprintf(", WAL %.1f MB", float64(r.WALBytes)/(1<<20))
	}
	return s
}

// Nightly runs the cleanup once a day at the configured hour while it
// is enabled -- config is re-read each time, so turning it on or off
// in the web admin needs no restart.
func Nightly(ctx context.Context, cfg func() config.MaintenanceConfig, d Deps, log Logger) {
	tick := time.NewTicker(5 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		c := cfg()
		ApplyLimits(c)
		now := time.Now()
		if !c.Enabled || now.Hour() != c.RunHour() {
			continue
		}
		if ran, err := RanOn(d.DB, now); err != nil || ran {
			continue
		}
		r := Run(ctx, d, c, false)
		if err := Save(d.DB, r); err != nil {
			log.Warn("maintenance: recording the run: %v", err)
		}
		log.Info("maintenance: deleted %s", r.Summary())
		for _, e := range r.Errors {
			log.Warn("maintenance: %s", e)
		}
	}
}

// pendingUsers deletes (or with dry, counts) the accounts that signed
// up before cutoff and were never approved -- bots, mostly.
func pendingUsers(db *sql.DB, cutoff time.Time, dry bool) (int, error) {
	users := user.NewStore(db)
	list, err := users.Pending()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, u := range list {
		if !u.CreatedAt.Before(cutoff) {
			continue
		}
		if !dry {
			if err := users.DeletePending(u.ID); err != nil {
				return n, err
			}
		}
		n++
	}
	return n, nil
}
