// Package applog is the BBS's shared activity/system log. The bbs and
// web daemons are separate processes, so log entries are persisted to
// the shared SQLite database (the same pattern internal/session uses
// for node tracking) rather than kept in memory, letting the web
// admin UI's log viewer show BBS daemon events too.
package applog

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// Level classifies a log entry's severity.
type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Entry is one persisted log line.
type Entry struct {
	ID       int64
	LoggedAt time.Time
	Source   string
	Level    Level
	Message  string
}

// Store persists log entries in the shared SQLite database.
type Store struct {
	db *sql.DB

	inserts int // best-effort trim counter; see trimEvery
}

// NewStore wraps an already-opened database handle (see internal/db).
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// trimEvery and maxRows bound the logs table's growth: after roughly
// every trimEvery inserts, rows beyond the most recent maxRows are
// pruned. A miscounted trim across a process restart is harmless --
// it just means the next trim happens a little early or late.
//
// These are vars rather than consts so tests can shrink them to
// exercise the trim path without inserting thousands of rows.
var (
	trimEvery = 50
	maxRows   = 5000
)

// SetMaxRows changes how many log entries are kept (config
// maintenance.log_keep_rows); n <= 0 leaves it as it is.
func SetMaxRows(n int) {
	if n > 0 {
		maxRows = n
	}
}

// Trim deletes (or with dry, only counts) the entries beyond the
// newest keep -- for internal/maintenance.
func (s *Store) Trim(keep int, dry bool) (int, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM logs`).Scan(&total); err != nil {
		return 0, fmt.Errorf("applog: count: %w", err)
	}
	n := total - keep
	if n <= 0 {
		return 0, nil
	}
	if dry {
		return n, nil
	}
	if _, err := s.db.Exec(`DELETE FROM logs WHERE id NOT IN (SELECT id FROM logs ORDER BY id DESC LIMIT ?)`, keep); err != nil {
		return 0, fmt.Errorf("applog: trim: %w", err)
	}
	return n, nil
}

func (s *Store) insert(source string, level Level, message string) error {
	if _, err := s.db.Exec(
		`INSERT INTO logs (source, level, message) VALUES (?, ?, ?)`,
		source, string(level), message,
	); err != nil {
		return fmt.Errorf("applog: insert: %w", err)
	}
	s.inserts++
	if s.inserts%trimEvery == 0 {
		if _, err := s.db.Exec(
			`DELETE FROM logs WHERE id NOT IN (SELECT id FROM logs ORDER BY id DESC LIMIT ?)`,
			maxRows,
		); err != nil {
			return fmt.Errorf("applog: trim: %w", err)
		}
	}
	return nil
}

// Recent returns the most recent entries, oldest first (so a caller
// can simply append them to a chronological list), up to limit.
func (s *Store) Recent(limit int) ([]Entry, error) {
	entries, err := s.query(`SELECT id, logged_at, source, level, message FROM logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries, nil
}

// Since returns entries logged after afterID, oldest first, up to
// limit -- for polling new entries after an initial Recent load.
func (s *Store) Since(afterID int64, limit int) ([]Entry, error) {
	return s.query(`SELECT id, logged_at, source, level, message FROM logs WHERE id > ? ORDER BY id ASC LIMIT ?`, afterID, limit)
}

// Categories the web admin's Logs page shows: a daemon (source), or
// for the bbs daemon its Telnet and SSH connections ("[telnet]" /
// "[ssh]" prefixes) apart from the rest (System: the daemon itself,
// doors and their background programs).
const (
	CategoryAll    = ""
	CategoryMailer = "mailer"
	CategorySystem = "system"
	CategoryTelnet = "telnet"
	CategorySSH    = "ssh"
	CategoryWeb    = "web"
)

// Filter narrows List. Zero values don't filter.
type Filter struct {
	Category string
	// Levels: any of these (e.g. warn and error together).
	Levels []Level
	// Prefixes: messages starting with any of these (a door's lines
	// start with its name and a colon); ExcludePrefixes: none of these.
	Prefixes        []string
	ExcludePrefixes []string
	// Query: messages containing this, ignoring case.
	Query string
	// BeforeID: older than this entry (paging back); AfterID: newer
	// (polling). Without either, the newest.
	BeforeID int64
	AfterID  int64
	Limit    int
}

// ErrUnknownCategory is a Filter.Category List doesn't know.
var ErrUnknownCategory = errors.New("applog: unknown category")

// List returns the entries matching f, oldest first -- the newest
// f.Limit of them, or with AfterID the oldest after it.
func (s *Store) List(f Filter) ([]Entry, error) {
	var where []string
	var args []any
	telnet := `substr(message, 1, 8) = '[telnet]'`
	ssh := `substr(message, 1, 5) = '[ssh]'`
	switch f.Category {
	case CategoryAll:
	case CategoryMailer, CategoryWeb:
		where, args = append(where, `source = ?`), append(args, f.Category)
	case CategoryTelnet:
		where = append(where, `source = 'bbs' AND `+telnet)
	case CategorySSH:
		where = append(where, `source = 'bbs' AND `+ssh)
	case CategorySystem:
		where = append(where, `source = 'bbs' AND NOT `+telnet+` AND NOT `+ssh)
	default:
		return nil, ErrUnknownCategory
	}
	if len(f.Levels) > 0 {
		marks := make([]string, len(f.Levels))
		for i, l := range f.Levels {
			marks[i] = "?"
			args = append(args, string(l))
		}
		where = append(where, `level IN (`+strings.Join(marks, ", ")+`)`)
	}
	if len(f.Prefixes) > 0 {
		var ors []string
		for _, p := range f.Prefixes {
			ors = append(ors, `substr(message, 1, length(?)) = ?`)
			args = append(args, p, p)
		}
		where = append(where, `(`+strings.Join(ors, " OR ")+`)`)
	}
	for _, p := range f.ExcludePrefixes {
		where = append(where, `substr(message, 1, length(?)) <> ?`)
		args = append(args, p, p)
	}
	if f.Query != "" {
		where = append(where, `instr(lower(message), lower(?)) > 0`)
		args = append(args, f.Query)
	}
	order := "DESC"
	switch {
	case f.AfterID > 0:
		where, args = append(where, `id > ?`), append(args, f.AfterID)
		order = "ASC"
	case f.BeforeID > 0:
		where, args = append(where, `id < ?`), append(args, f.BeforeID)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 200
	}
	q := `SELECT id, logged_at, source, level, message FROM logs`
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	q += ` ORDER BY id ` + order + ` LIMIT ?`
	entries, err := s.query(q, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	if order == "DESC" {
		for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
			entries[i], entries[j] = entries[j], entries[i]
		}
	}
	return entries, nil
}

func (s *Store) query(sqlQuery string, args ...any) ([]Entry, error) {
	rows, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("applog: query: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var level string
		if err := rows.Scan(&e.ID, &e.LoggedAt, &e.Source, &level, &e.Message); err != nil {
			return nil, fmt.Errorf("applog: scan: %w", err)
		}
		e.Level = Level(level)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("applog: query: %w", err)
	}
	return entries, nil
}

// Logger writes entries both to the process's standard log output (so
// an operator tailing the process still sees them) and to the shared
// Store (so the web admin UI can show them). source identifies which
// daemon wrote the entry, e.g. "bbs" or "web".
type Logger struct {
	store  *Store
	source string
}

// NewLogger returns a Logger that tags every entry it writes with
// source.
func NewLogger(store *Store, source string) *Logger {
	return &Logger{store: store, source: source}
}

// Info logs a routine event (e.g. a session connecting, an area being
// created).
func (l *Logger) Info(format string, args ...any) { l.write(LevelInfo, format, args...) }

// Warn logs a recoverable problem worth a sysop's attention.
func (l *Logger) Warn(format string, args ...any) { l.write(LevelWarn, format, args...) }

// Error logs a failure that didn't necessarily stop the process.
func (l *Logger) Error(format string, args ...any) { l.write(LevelError, format, args...) }

// Fatal logs at error level and then exits the process, mirroring the
// standard library's log.Fatalf for the daemon-startup call sites
// that used it before this package existed.
func (l *Logger) Fatal(format string, args ...any) {
	l.write(LevelError, format, args...)
	os.Exit(1)
}

func (l *Logger) write(level Level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[%s] %s", level, msg)
	if err := l.store.insert(l.source, level, msg); err != nil {
		log.Printf("applog: %v", err)
	}
}
