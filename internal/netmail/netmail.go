// Package netmail implements private, per-recipient mail between
// users -- the BBS's Netmail feature, distinct from the shared,
// topic-based message areas (Echomail) internal/message implements.
//
// It exists ahead of schedule, before the BinkP/FidoNet mailer
// (internal/mail, still an empty Phase 2 placeholder per CLAUDE.md's
// roadmap), so the data model doesn't need a later migration: every
// message carries optional From/To FTN addresses (zone:net/node.point)
// alongside the local recipient. A message addressed to a remote
// system just sits with ToUserID unset ("queued") until the future
// mailer/tosser exists to actually route it -- nothing here transmits
// anything over BinkP yet.
package netmail

import (
	"database/sql"
	"fmt"
	"time"
)

// Message is one netmail message. Unlike an echo-area message (see
// internal/message), it always has exactly one recipient, so read
// state lives directly on the row (ReadAt) instead of needing a
// separate per-user reads table the way message_reads/file_reads do.
type Message struct {
	ID          int64
	FromUserID  int64
	FromName    string // joined from users.username for display
	FromAddress string // this BBS's own FTN address when sent, may be empty if unconfigured
	ToUserID    sql.NullInt64
	ToName      string // the local username, or the raw FTN address if unresolved/remote
	ToAddress   string // FTN address (zone:net/node.point); empty means purely local
	Subject     string
	Body        string
	PostedAt    time.Time
	ReadAt      sql.NullTime
}

// IsLocal reports whether m resolved to a user on this BBS.
func (m *Message) IsLocal() bool { return m.ToUserID.Valid }

// IsRead reports whether the recipient has opened m in the reader.
func (m *Message) IsRead() bool { return m.ReadAt.Valid }

// Store persists netmail Messages in the shared SQLite database.
type Store struct {
	db *sql.DB
}

// NewStore wraps an already-open database connection.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Send stores a new netmail message. toUserID is 0 when the recipient
// couldn't be resolved to a local user, in which case toAddress must
// carry the intended FTN address -- it stays undeliverable until a
// BinkP mailer exists to route it.
func (s *Store) Send(fromUserID int64, fromAddress string, toUserID int64, toName, toAddress, subject, body string) (*Message, error) {
	var toUserIDArg any
	if toUserID > 0 {
		toUserIDArg = toUserID
	}
	res, err := s.db.Exec(
		`INSERT INTO netmail_messages (from_user_id, from_address, to_user_id, to_name, to_address, subject, body)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		fromUserID, fromAddress, toUserIDArg, toName, toAddress, subject, body,
	)
	if err != nil {
		return nil, fmt.Errorf("netmail: send: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("netmail: send: %w", err)
	}
	return s.MessageByID(id)
}

// MessageByID loads a single message, with its sender's current
// username joined in as FromName.
func (s *Store) MessageByID(id int64) (*Message, error) {
	row := s.db.QueryRow(
		`SELECT m.id, m.from_user_id, u.username, m.from_address,
		        m.to_user_id, m.to_name, m.to_address, m.subject, m.body, m.posted_at, m.read_at
		 FROM netmail_messages m JOIN users u ON u.id = m.from_user_id
		 WHERE m.id = ?`, id,
	)
	var m Message
	if err := row.Scan(&m.ID, &m.FromUserID, &m.FromName, &m.FromAddress,
		&m.ToUserID, &m.ToName, &m.ToAddress, &m.Subject, &m.Body, &m.PostedAt, &m.ReadAt); err != nil {
		return nil, fmt.Errorf("netmail: load %d: %w", id, err)
	}
	return &m, nil
}

// Inbox returns userID's received netmail, newest first -- real
// netmail readers conventionally show the most recent message first,
// the opposite of an echo area's oldest-first message list.
func (s *Store) Inbox(userID int64) ([]Message, error) {
	rows, err := s.db.Query(
		`SELECT m.id, m.from_user_id, u.username, m.from_address,
		        m.to_user_id, m.to_name, m.to_address, m.subject, m.body, m.posted_at, m.read_at
		 FROM netmail_messages m JOIN users u ON u.id = m.from_user_id
		 WHERE m.to_user_id = ?
		 ORDER BY m.posted_at DESC, m.id DESC`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("netmail: inbox for user %d: %w", userID, err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.FromUserID, &m.FromName, &m.FromAddress,
			&m.ToUserID, &m.ToName, &m.ToAddress, &m.Subject, &m.Body, &m.PostedAt, &m.ReadAt); err != nil {
			return nil, fmt.Errorf("netmail: scan inbox row: %w", err)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("netmail: inbox for user %d: %w", userID, err)
	}
	return msgs, nil
}

// UnreadCount returns how many of userID's received messages haven't
// been opened yet, for the main menu's Netmail item label.
func (s *Store) UnreadCount(userID int64) (int, error) {
	var n int
	if err := s.db.QueryRow(
		`SELECT COUNT(1) FROM netmail_messages WHERE to_user_id = ? AND read_at IS NULL`, userID,
	).Scan(&n); err != nil {
		return 0, fmt.Errorf("netmail: unread count for user %d: %w", userID, err)
	}
	return n, nil
}

// MarkRead records that messageID has been opened in the reader.
// Idempotent: reading the same message again is a no-op.
func (s *Store) MarkRead(messageID int64) error {
	if _, err := s.db.Exec(
		`UPDATE netmail_messages SET read_at = CURRENT_TIMESTAMP WHERE id = ? AND read_at IS NULL`,
		messageID,
	); err != nil {
		return fmt.Errorf("netmail: mark %d read: %w", messageID, err)
	}
	return nil
}
