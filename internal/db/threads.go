package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// backfillThreads threads the messages stored before replies were
// linked: their REPLY kludges are gone, so a "Re:" subject joins the
// first message with the same subject in its area (reply_guess). Runs
// once per database.
func backfillThreads(db *sql.DB) error {
	res, err := db.Exec(`INSERT OR IGNORE INTO meta (key, value) VALUES ('threads_backfilled', ?)`, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("db: threads backfill: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil // done before
	}
	rows, err := db.Query(`SELECT id, area_id, subject FROM messages WHERE reply_to IS NULL ORDER BY area_id, posted_at, id`)
	if err != nil {
		return fmt.Errorf("db: threads backfill: %w", err)
	}
	type link struct{ id, parent int64 }
	var links []link
	roots := map[int64]map[string]int64{}
	for rows.Next() {
		var id, area int64
		var subject string
		if err := rows.Scan(&id, &area, &subject); err != nil {
			rows.Close()
			return fmt.Errorf("db: threads backfill: %w", err)
		}
		base, re := BaseSubject(subject)
		if base == "" {
			continue
		}
		if roots[area] == nil {
			roots[area] = map[string]int64{}
		}
		if root, ok := roots[area][base]; ok && re {
			links = append(links, link{id, root})
		} else if !ok {
			roots[area][base] = id
		}
	}
	rows.Close()
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("db: threads backfill: %w", err)
	}
	defer tx.Rollback()
	for _, l := range links {
		if _, err := tx.Exec(`UPDATE messages SET reply_to = ?, reply_guess = 1 WHERE id = ?`, l.parent, l.id); err != nil {
			return fmt.Errorf("db: threads backfill: %w", err)
		}
	}
	return tx.Commit()
}

// BaseSubject strips reply prefixes ("Re:", "RE^2:", "Re[3]:", "AW:")
// and folds case and spaces: the subject a thread shares. re reports
// whether there was a prefix.
func BaseSubject(subject string) (base string, re bool) {
	s := strings.TrimSpace(subject)
	for {
		l := strings.ToLower(s)
		n := 0
		switch {
		case strings.HasPrefix(l, "re"):
			n = 2
		case strings.HasPrefix(l, "aw"):
			n = 2
		default:
			return strings.Join(strings.Fields(strings.ToLower(s)), " "), re
		}
		rest := s[n:]
		// Re^2: / Re[2]: / Re(2):
		if len(rest) > 0 && strings.ContainsRune("^[(", rune(rest[0])) {
			j := 1
			for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
				j++
			}
			if j < len(rest) && (rest[j] == ']' || rest[j] == ')') {
				j++
			}
			rest = rest[j:]
		}
		if !strings.HasPrefix(rest, ":") {
			return strings.Join(strings.Fields(strings.ToLower(s)), " "), re
		}
		s, re = strings.TrimSpace(rest[1:]), true
	}
}
