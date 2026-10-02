package message

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/db"
)

// Threads: every message may name the one it answers (ReplyTo, same
// area). Remote mail says so in its REPLY kludge (matched against the
// MSGIDs here, including the ones our own posts went out under); a
// reply written here links directly; anything else whose subject
// starts with "Re:" joins the first message with the same subject in
// the last guessWindow (ReplyGuess) -- older mail lost its kludges.

// guessWindow bounds how far back a "Re:" subject looks for its thread.
const guessWindow = 120 * 24 * time.Hour

// Thread links a newly stored message into its thread: by replyMsgID
// (a REPLY kludge, "" if none), else by subject; and adopts replies
// that arrived before it.
func (s *Store) Thread(m *Message, replyMsgID string) error {
	var parent int64
	if replyMsgID != "" {
		err := s.db.QueryRow(`SELECT id FROM messages WHERE area_id = ? AND id != ? AND (msgid = ? OR out_msgid = ?) LIMIT 1`,
			m.AreaID, m.ID, replyMsgID, replyMsgID).Scan(&parent)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("message: thread %d: %w", m.ID, err)
		}
	}
	guess := false
	if parent == 0 {
		p, err := s.guessParent(m)
		if err != nil {
			return err
		}
		parent, guess = p, p != 0
	}
	var replyTo any
	if parent != 0 {
		replyTo = parent
	}
	if _, err := s.db.Exec(`UPDATE messages SET reply_to = ?, reply_guess = ?, reply_msgid = ? WHERE id = ?`,
		replyTo, guess, replyMsgID, m.ID); err != nil {
		return fmt.Errorf("message: thread %d: %w", m.ID, err)
	}
	m.ReplyTo, m.ReplyGuess, m.ReplyMsgID = sql.NullInt64{Int64: parent, Valid: parent != 0}, guess, replyMsgID
	if m.MsgID != "" {
		if _, err := s.db.Exec(`UPDATE messages SET reply_to = ?, reply_guess = 0
			WHERE area_id = ? AND reply_msgid = ? AND id != ? AND (reply_to IS NULL OR reply_guess = 1)`,
			m.ID, m.AreaID, m.MsgID, m.ID); err != nil {
			return fmt.Errorf("message: thread %d: %w", m.ID, err)
		}
	}
	return nil
}

// guessParent: the thread root for a "Re:" subject, or 0.
func (s *Store) guessParent(m *Message) (int64, error) {
	base, re := db.BaseSubject(m.Subject)
	if !re || base == "" {
		return 0, nil
	}
	// Narrowed in SQL by the subject's last word, matched exactly in Go.
	words := strings.Fields(base)
	tail := words[len(words)-1]
	rows, err := s.db.Query(`SELECT id, subject, reply_to FROM messages
		WHERE area_id = ?1 AND INSTR(LOWER(subject), ?3) > 0
			AND (posted_at, id) < (SELECT posted_at, id FROM messages WHERE id = ?2)
			AND posted_at >= datetime((SELECT substr(posted_at, 1, 19) FROM messages WHERE id = ?2), ?4)
		ORDER BY posted_at, id LIMIT 200`,
		m.AreaID, m.ID, tail, fmt.Sprintf("-%d days", int(guessWindow.Hours()/24)))
	if err != nil {
		return 0, fmt.Errorf("message: thread %d: %w", m.ID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var subject string
		var replyTo sql.NullInt64
		if err := rows.Scan(&id, &subject, &replyTo); err != nil {
			return 0, fmt.Errorf("message: thread %d: %w", m.ID, err)
		}
		if b, _ := db.BaseSubject(subject); b == base {
			if replyTo.Valid {
				return replyTo.Int64, nil
			}
			return id, nil
		}
	}
	return 0, rows.Err()
}

// SetReplyTo links id as a reply written here to parentID.
func (s *Store) SetReplyTo(id, parentID int64) error {
	if _, err := s.db.Exec(`UPDATE messages SET reply_to = ?, reply_guess = 0, reply_msgid = '' WHERE id = ?`, parentID, id); err != nil {
		return fmt.Errorf("message: reply %d to %d: %w", id, parentID, err)
	}
	return nil
}

// SetOutMsgID records the MSGID a local post went out under.
func (s *Store) SetOutMsgID(id int64, msgID string) error {
	if _, err := s.db.Exec(`UPDATE messages SET out_msgid = ? WHERE id = ? AND out_msgid != ?`, msgID, id, msgID); err != nil {
		return fmt.Errorf("message: out msgid %d: %w", id, err)
	}
	return nil
}

// ThreadEntry is one message of a thread, without its body.
type ThreadEntry struct {
	ID       int64
	ReplyTo  int64 // 0 for the root
	Depth    int
	FromName string
	ToName   string
	Subject  string
	PostedAt time.Time
	Guessed  bool // linked by its "Re:" subject only
}

// ThreadOf returns the whole thread id belongs to, in reading order:
// depth-first, each reply after its parent, siblings by date.
func (s *Store) ThreadOf(id int64) ([]ThreadEntry, error) {
	root := id
	for range 200 { // guard against a loop in damaged data
		var p sql.NullInt64
		if err := s.db.QueryRow(`SELECT reply_to FROM messages WHERE id = ?`, root).Scan(&p); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				break // parent expired: the oldest left is the root
			}
			return nil, fmt.Errorf("message: thread of %d: %w", id, err)
		}
		if !p.Valid || p.Int64 == root {
			break
		}
		var exists int
		if s.db.QueryRow(`SELECT 1 FROM messages WHERE id = ?`, p.Int64).Scan(&exists) != nil {
			break
		}
		root = p.Int64
	}
	rows, err := s.db.Query(`WITH RECURSIVE t(id) AS (
			SELECT ? UNION SELECT m.id FROM messages m JOIN t ON m.reply_to = t.id
		)
		SELECT m.id, COALESCE(m.reply_to, 0), COALESCE(NULLIF(m.from_name, ''), u.username), m.to_name, m.subject, m.posted_at, m.reply_guess
		FROM t JOIN messages m ON m.id = t.id LEFT JOIN users u ON u.id = m.from_user_id
		LIMIT 2000`, root)
	if err != nil {
		return nil, fmt.Errorf("message: thread of %d: %w", id, err)
	}
	defer rows.Close()
	byID := map[int64]*ThreadEntry{}
	kids := map[int64][]int64{}
	for rows.Next() {
		var e ThreadEntry
		var from sql.NullString
		if err := rows.Scan(&e.ID, &e.ReplyTo, &from, &e.ToName, &e.Subject, &e.PostedAt, &e.Guessed); err != nil {
			return nil, fmt.Errorf("message: thread of %d: %w", id, err)
		}
		e.FromName = from.String
		if e.ID == root {
			e.ReplyTo, e.Guessed = 0, false
		}
		byID[e.ID] = &e
		if e.ID != root {
			kids[e.ReplyTo] = append(kids[e.ReplyTo], e.ID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []ThreadEntry
	var walk func(id int64, depth int)
	walk = func(id int64, depth int) {
		e := byID[id]
		if e == nil || len(out) > len(byID) {
			return
		}
		e.Depth = depth
		out = append(out, *e)
		k := kids[id]
		sort.Slice(k, func(i, j int) bool {
			a, b := byID[k[i]], byID[k[j]]
			if !a.PostedAt.Equal(b.PostedAt) {
				return a.PostedAt.Before(b.PostedAt)
			}
			return a.ID < b.ID
		})
		for _, c := range k {
			walk(c, depth+1)
		}
	}
	walk(root, 0)
	return out, nil
}

// ThreadSummary is one thread of an area: its first message (still
// here) and how much happened since.
type ThreadSummary struct {
	Root     Message // without Body
	Replies  int
	Unread   int
	LastAt   time.Time
	LastFrom string
}

// AreaThreads lists areaID's threads, most recently active first, a
// page at a time; total is the number of threads. unread counts for
// userID.
func (s *Store) AreaThreads(areaID, userID int64, limit, offset int) (threads []ThreadSummary, total int, err error) {
	// Each message's root: follow reply_to (recursive), stopping where
	// the parent is gone.
	const roots = `WITH RECURSIVE r(id, root, n) AS (
			SELECT m.id, m.id, 0 FROM messages m
			WHERE m.area_id = ?1 AND (m.reply_to IS NULL OR NOT EXISTS (SELECT 1 FROM messages p WHERE p.id = m.reply_to))
			UNION ALL
			SELECT m.id, r.root, r.n + 1 FROM messages m JOIN r ON m.reply_to = r.id WHERE r.n < 200 AND m.id != r.root
		)`
	if err := s.db.QueryRow(roots+` SELECT COUNT(DISTINCT root) FROM r`, areaID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("message: threads of area %d: %w", areaID, err)
	}
	rows, err := s.db.Query(roots+`, agg AS (
			SELECT r.root, COUNT(*) - 1 AS replies, MAX(substr(m.posted_at, 1, 19)) AS last_at,
				SUM(CASE WHEN mr.message_id IS NULL THEN 1 ELSE 0 END) AS unread
			FROM r JOIN messages m ON m.id = r.id
			LEFT JOIN message_reads mr ON mr.message_id = m.id AND mr.user_id = ?2
			GROUP BY r.root
		)
		SELECT m.id, m.area_id, m.from_user_id, COALESCE(NULLIF(m.from_name, ''), u.username), m.to_name, m.subject, m.posted_at,
			agg.replies, agg.unread, agg.last_at,
			(SELECT COALESCE(NULLIF(l.from_name, ''), lu.username) FROM r lr JOIN messages l ON l.id = lr.id LEFT JOIN users lu ON lu.id = l.from_user_id
				WHERE lr.root = agg.root ORDER BY l.posted_at DESC, l.id DESC LIMIT 1)
		FROM agg JOIN messages m ON m.id = agg.root LEFT JOIN users u ON u.id = m.from_user_id
		ORDER BY agg.last_at DESC, m.id DESC
		LIMIT ?3 OFFSET ?4`, areaID, userID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("message: threads of area %d: %w", areaID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var t ThreadSummary
		var from, lastFrom sql.NullString
		var lastAt string
		if err := rows.Scan(&t.Root.ID, &t.Root.AreaID, &t.Root.FromUserID, &from, &t.Root.ToName, &t.Root.Subject, &t.Root.PostedAt,
			&t.Replies, &t.Unread, &lastAt, &lastFrom); err != nil {
			return nil, 0, fmt.Errorf("message: threads of area %d: %w", areaID, err)
		}
		t.Root.FromName, t.LastFrom = from.String, lastFrom.String
		t.LastAt = parseTime(lastAt)
		threads = append(threads, t)
	}
	return threads, total, rows.Err()
}

// parseTime reads the first 19 characters of a stored TIMESTAMP, which
// are UTC whichever way it was written ("2026-09-01 12:00:00" from
// CURRENT_TIMESTAMP, "... +0000 UTC" from a bound time).
func parseTime(v string) time.Time {
	t, _ := time.Parse("2006-01-02 15:04:05", strings.TrimSpace(v))
	return t
}
