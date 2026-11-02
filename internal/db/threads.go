package db

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
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

// GuessWindow bounds how far back a "Re:" subject looks for what it
// answers.
const GuessWindow = 120 * 24 * time.Hour

// Querier is what GuessParent reads with: a *sql.DB or a *sql.Tx.
type Querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// GuessParent is the message a reply without a (known) REPLY kludge
// most likely answers, from its "Re:" subject, or 0: the latest earlier
// message in its area with the same subject within GuessWindow -- by
// whoever it's addressed to, if one of them wrote such a message. Not
// the thread's first message: a subject like "test" comes up again and
// again, months apart, and would gather them all under one.
func GuessParent(q Querier, areaID, id int64, subject, toName string) (int64, error) {
	base, re := BaseSubject(subject)
	if !re || base == "" {
		return 0, nil
	}
	// Narrowed in SQL by the subject's last word, matched exactly in Go.
	// SQLite's LOWER folds ASCII only: a word with more isn't narrowed by.
	words := strings.Fields(base)
	tail := words[len(words)-1]
	for _, r := range tail {
		if r >= utf8.RuneSelf {
			tail = ""
			break
		}
	}
	rows, err := q.Query(`SELECT m.id, m.subject, COALESCE(NULLIF(m.from_name, ''), u.username, '')
		FROM messages m LEFT JOIN users u ON u.id = m.from_user_id
		WHERE m.area_id = ?1 AND INSTR(LOWER(m.subject), ?3) > 0
			AND (m.posted_at, m.id) < (SELECT posted_at, id FROM messages WHERE id = ?2)
			AND m.posted_at >= datetime((SELECT substr(posted_at, 1, 19) FROM messages WHERE id = ?2), ?4)
		ORDER BY m.posted_at DESC, m.id DESC LIMIT 200`,
		areaID, id, tail, fmt.Sprintf("-%d days", int(GuessWindow.Hours()/24)))
	if err != nil {
		return 0, fmt.Errorf("db: guess parent of %d: %w", id, err)
	}
	defer rows.Close()
	to := strings.TrimSpace(toName)
	if strings.EqualFold(to, "all") {
		to = ""
	}
	var latest int64
	for rows.Next() {
		var cand int64
		var subj, from string
		if err := rows.Scan(&cand, &subj, &from); err != nil {
			return 0, fmt.Errorf("db: guess parent of %d: %w", id, err)
		}
		if b, _ := BaseSubject(subj); b != base {
			continue
		}
		if to == "" || strings.EqualFold(strings.TrimSpace(from), to) {
			return cand, nil
		}
		if latest == 0 {
			latest = cand
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("db: guess parent of %d: %w", id, err)
	}
	return latest, nil
}

// reguessThreads redoes the subject guesses with GuessParent -- they
// used to join a thread's first message, however old -- and links the
// replies to our own posts from before out_msgid was recorded. Runs
// once per database.
func reguessThreads(db *sql.DB) error {
	res, err := db.Exec(`INSERT OR IGNORE INTO meta (key, value) VALUES ('threads_reguessed', ?)`, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("db: threads reguess: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil // done before
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("db: threads reguess: %w", err)
	}
	defer tx.Rollback()

	// A REPLY to "<our address> <id in hex>": the MSGID our post id
	// went out under (see the tosser's localMsgID).
	type own struct {
		id, parent int64
		msgid      string
	}
	var owns []own
	rows, err := tx.Query(`SELECT id, area_id, reply_msgid FROM messages
		WHERE reply_msgid != '' AND (reply_to IS NULL OR reply_guess = 1)`)
	if err != nil {
		return fmt.Errorf("db: threads reguess: %w", err)
	}
	type orphan struct {
		id, area int64
		msgid    string
	}
	var orphans []orphan
	for rows.Next() {
		var o orphan
		if err := rows.Scan(&o.id, &o.area, &o.msgid); err != nil {
			rows.Close()
			return fmt.Errorf("db: threads reguess: %w", err)
		}
		orphans = append(orphans, o)
	}
	rows.Close()
	for _, o := range orphans {
		f := strings.Fields(o.msgid)
		if len(f) != 2 || len(f[1]) != 8 {
			continue
		}
		pid, err := strconv.ParseInt(f[1], 16, 64)
		if err != nil {
			continue
		}
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM messages WHERE id = ? AND area_id = ? AND from_user_id IS NOT NULL AND out_msgid = ''`,
			pid, o.area).Scan(&n); err != nil {
			return fmt.Errorf("db: threads reguess: %w", err)
		}
		if n == 1 {
			owns = append(owns, own{o.id, pid, o.msgid})
		}
	}
	for _, o := range owns {
		if _, err := tx.Exec(`UPDATE messages SET out_msgid = ? WHERE id = ?`, o.msgid, o.parent); err != nil {
			return fmt.Errorf("db: threads reguess: %w", err)
		}
		if _, err := tx.Exec(`UPDATE messages SET reply_to = ?, reply_guess = 0 WHERE id = ?`, o.parent, o.id); err != nil {
			return fmt.Errorf("db: threads reguess: %w", err)
		}
	}

	type guess struct {
		id, area    int64
		subject, to string
		replyTo     sql.NullInt64
	}
	var all []guess
	rows, err = tx.Query(`SELECT id, area_id, subject, to_name, reply_to FROM messages WHERE reply_to IS NULL OR reply_guess = 1`)
	if err != nil {
		return fmt.Errorf("db: threads reguess: %w", err)
	}
	for rows.Next() {
		var g guess
		if err := rows.Scan(&g.id, &g.area, &g.subject, &g.to, &g.replyTo); err != nil {
			rows.Close()
			return fmt.Errorf("db: threads reguess: %w", err)
		}
		all = append(all, g)
	}
	rows.Close()
	for _, g := range all {
		p, err := GuessParent(tx, g.area, g.id, g.subject, g.to)
		if err != nil {
			return err
		}
		if p == g.replyTo.Int64 {
			continue
		}
		var replyTo any
		if p != 0 {
			replyTo = p
		}
		if _, err := tx.Exec(`UPDATE messages SET reply_to = ?, reply_guess = ? WHERE id = ?`, replyTo, p != 0, g.id); err != nil {
			return fmt.Errorf("db: threads reguess: %w", err)
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
