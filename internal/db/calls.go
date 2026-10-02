package db

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	logTelnetLogin = regexp.MustCompile(`^\[(telnet|ssh)\] node \d+: (.+) logged in$`)
	logWebLogin    = regexp.MustCompile(`^(.+) logged into the BBS web portal$`)
)

// backfillCalls fills the call log, once, from the login lines still
// in the system log -- so the statistics don't start empty.
func backfillCalls(db *sql.DB) error {
	res, err := db.Exec(`INSERT OR IGNORE INTO meta (key, value) VALUES ('calls_backfilled', ?)`, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("db: calls backfill: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	ids := map[string]int64{}
	rows, err := db.Query(`SELECT id, username FROM users`)
	if err != nil {
		return fmt.Errorf("db: calls backfill: %w", err)
	}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return fmt.Errorf("db: calls backfill: %w", err)
		}
		ids[strings.ToLower(name)] = id
	}
	rows.Close()

	type call struct {
		user int64
		via  string
		at   string
	}
	var calls []call
	lastWeb := map[int64]time.Time{}
	rows, err = db.Query(`SELECT substr(logged_at, 1, 19), message FROM logs WHERE message LIKE '%logged in%' ORDER BY id`)
	if err != nil {
		return fmt.Errorf("db: calls backfill: %w", err)
	}
	for rows.Next() {
		var at, msg string
		if err := rows.Scan(&at, &msg); err != nil {
			rows.Close()
			return fmt.Errorf("db: calls backfill: %w", err)
		}
		if m := logTelnetLogin.FindStringSubmatch(msg); m != nil {
			if id, ok := ids[strings.ToLower(m[2])]; ok {
				calls = append(calls, call{id, m[1], at})
			}
		} else if m := logWebLogin.FindStringSubmatch(msg); m != nil {
			id, ok := ids[strings.ToLower(m[1])]
			t, err := time.Parse("2006-01-02 15:04:05", at)
			// Logins within WebCallGap count once, as RecordCall does.
			if ok && err == nil && t.Sub(lastWeb[id]) >= WebCallGap {
				lastWeb[id] = t
				calls = append(calls, call{id, "web", at})
			}
		}
	}
	rows.Close()
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("db: calls backfill: %w", err)
	}
	defer tx.Rollback()
	for _, c := range calls {
		if _, err := tx.Exec(`INSERT INTO calls (user_id, via, at) VALUES (?, ?, ?)`, c.user, c.via, c.at); err != nil {
			return fmt.Errorf("db: calls backfill: %w", err)
		}
	}
	return tx.Commit()
}

// WebCallGap: web logins of the same caller closer than this are one
// call (the reader app logs in again now and then).
const WebCallGap = 30 * time.Minute
