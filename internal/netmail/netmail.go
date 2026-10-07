// Package netmail implements private, per-recipient mail between
// users -- the BBS's Netmail feature, distinct from the shared,
// topic-based message areas (Echomail) internal/message implements.
//
// Its schema anticipated the BinkP/FidoNet mailer from the start (see
// CLAUDE.md's roadmap): every message carries optional From/To FTN
// addresses (zone:net/node.point) alongside the local recipient. A
// message addressed to a remote system sits with ToUserID unset and
// SentAt unset ("queued") until internal/tosser hands it to a
// configured uplink; mail arriving from a remote system the same way
// (via Receive) has FromUserID unset instead, since there's no local
// sender account for it.
package netmail

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/midrei/nullmodem-bbs/internal/textclean"
	"strconv"
	"strings"
	"time"
)

// Message is one netmail message. Unlike an echo-area message (see
// internal/message), it always has exactly one recipient, so read
// state lives directly on the row (ReadAt) instead of needing a
// separate per-user reads table the way message_reads/file_reads do.
type Message struct {
	ID          int64
	FromUserID  sql.NullInt64
	FromName    string // joined from users.username for a local sender, or the stored name for a remote one (see Receive)
	FromAddress string // sender's FTN address, may be empty if unconfigured (local sender) or unknown (remote sender)
	ToUserID    sql.NullInt64
	ToName      string // the local username, or the raw FTN address if unresolved/remote
	ToAddress   string // FTN address (zone:net/node.point); empty means purely local
	Subject     string
	Body        string
	PostedAt    time.Time
	ReadAt      sql.NullTime
	SentAt      sql.NullTime
	// Crash marks a remote-addressed message for priority delivery
	// (FTS-0001's AttrCrash): internal/tosser routes it to a specific
	// crash-only uplink and dials that uplink immediately instead of
	// waiting for the next scheduled poll. Meaningless for local mail.
	Crash bool
	// Email is the other side's address of a mail through the email
	// gateway (internal/emailgw): the recipient of one written here
	// (FromUserID set), the sender of one that came in. "" for netmail.
	Email string
}

// IsEmail reports whether m went or came through the email gateway.
func (m *Message) IsEmail() bool { return m.Email != "" }

// IsLocal reports whether m resolved to a user on this BBS.
func (m *Message) IsLocal() bool { return m.ToUserID.Valid }

// IsRead reports whether the recipient has opened m in the reader.
func (m *Message) IsRead() bool { return m.ReadAt.Valid }

// IsSent reports whether m has been handed off to (and acknowledged
// by) an uplink -- always true for local mail, meaningful only for a
// message with ToAddress set and no ToUserID.
func (m *Message) IsSent() bool { return m.SentAt.Valid }

// IsFromRemote reports whether m arrived from a remote FTN system via
// internal/tosser rather than being composed by a local user.
func (m *Message) IsFromRemote() bool { return !m.FromUserID.Valid }

// Store persists netmail Messages in the shared SQLite database.
type Store struct {
	db *sql.DB
}

// NewStore wraps an already-open database connection.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Send stores a new netmail message. toUserID is 0 when the recipient
// couldn't be resolved to a local user, in which case toAddress must
// carry the intended FTN address -- it stays undeliverable until a
// BinkP mailer exists to route it. crash is meaningless for a local
// recipient; for a remote one, it flags the message for
// internal/tosser's priority routing (see Message.Crash).
func (s *Store) Send(fromUserID int64, fromAddress string, toUserID int64, toName, toAddress, subject, body string, crash bool) (*Message, error) {
	toName, subject = textclean.Line(toName), textclean.Line(subject)
	var toUserIDArg any
	if toUserID > 0 {
		toUserIDArg = toUserID
	}
	res, err := s.db.Exec(
		`INSERT INTO netmail_messages (from_user_id, from_address, to_user_id, to_name, to_address, subject, body, crash)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		fromUserID, fromAddress, toUserIDArg, toName, toAddress, subject, body, crash,
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

// SendSystem queues a system-composed netmail message -- one with no
// local sender account, like an Areafix/Filefix subscription request
// (see internal/areafix) -- mirroring how Receive handles a remote
// sender with no local account, but for a message WE originate rather
// than one arriving from elsewhere. fromName is stored directly (e.g.
// "Areafix", so a reply naming us back is recognizable) rather than
// joined from a users row. crash is meaningless for a local
// recipient; for a remote one it flags the message for
// internal/tosser's priority routing (see Message.Crash) -- almost
// always what a system-composed message wants, since there's no
// waiting caller and no reason to sit until the next scheduled poll.
func (s *Store) SendSystem(fromName, fromAddress, toName, toAddress, subject, body string, crash bool) (*Message, error) {
	fromName, toName, subject = textclean.Line(fromName), textclean.Line(toName), textclean.Line(subject)
	res, err := s.db.Exec(
		`INSERT INTO netmail_messages (from_user_id, from_name, from_address, to_name, to_address, subject, body, crash)
		 VALUES (NULL, ?, ?, ?, ?, ?, ?, ?)`,
		fromName, fromAddress, toName, toAddress, subject, body, crash,
	)
	if err != nil {
		return nil, fmt.Errorf("netmail: send system: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("netmail: send system: %w", err)
	}
	return s.MessageByID(id)
}

// Receive stores a netmail message that arrived from a remote FTN
// system via internal/tosser -- Send's counterpart for locally
// composed mail. There's no local from_user_id for a remote sender,
// so fromName is stored directly instead of being joined from
// users.username at read time. postedAt is the message's own Written
// timestamp from the packet, not when we happened to receive it.
func (s *Store) Receive(fromName, fromAddress string, toUserID int64, toName, toAddress, subject, body string, postedAt time.Time, crash bool) (*Message, error) {
	fromName, toName, subject = textclean.Line(fromName), textclean.Line(toName), textclean.Line(subject)
	var toUserIDArg any
	if toUserID > 0 {
		toUserIDArg = toUserID
	}
	res, err := s.db.Exec(
		`INSERT INTO netmail_messages (from_user_id, from_name, from_address, to_user_id, to_name, to_address, subject, body, posted_at, crash)
		 VALUES (NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		// UTC, like CURRENT_TIMESTAMP: posted_at is compared as text.
		fromName, fromAddress, toUserIDArg, toName, toAddress, subject, body, postedAt.UTC(), crash,
	)
	if err != nil {
		return nil, fmt.Errorf("netmail: receive: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("netmail: receive: %w", err)
	}
	return s.MessageByID(id)
}

// PendingOutbound returns netmail addressed to a remote FTN system
// that hasn't been handed to an uplink yet, oldest first -- what
// internal/tosser bundles into an outbound packet on each poll.
func (s *Store) PendingOutbound() ([]Message, error) {
	rows, err := s.db.Query(
		`SELECT m.id, m.from_user_id, COALESCE(u.username, m.from_name) AS from_name, m.from_address,
		        m.to_user_id, m.to_name, m.to_address, m.subject, m.body, m.posted_at, m.read_at, m.sent_at, m.crash, m.email
		 FROM netmail_messages m LEFT JOIN users u ON u.id = m.from_user_id
		 WHERE m.to_user_id IS NULL AND m.to_address != '' AND m.sent_at IS NULL
		 ORDER BY m.posted_at ASC, m.id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("netmail: pending outbound: %w", err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.FromUserID, &m.FromName, &m.FromAddress,
			&m.ToUserID, &m.ToName, &m.ToAddress, &m.Subject, &m.Body, &m.PostedAt, &m.ReadAt, &m.SentAt, &m.Crash, &m.Email); err != nil {
			return nil, fmt.Errorf("netmail: scan pending outbound row: %w", err)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("netmail: pending outbound: %w", err)
	}
	return msgs, nil
}

// MarkSent records that messageID was successfully handed off to (and
// acknowledged by) an uplink -- see internal/tosser.Poll. Idempotent.
func (s *Store) MarkSent(messageID int64) error {
	if _, err := s.db.Exec(
		`UPDATE netmail_messages SET sent_at = CURRENT_TIMESTAMP WHERE id = ? AND sent_at IS NULL`,
		messageID,
	); err != nil {
		return fmt.Errorf("netmail: mark %d sent: %w", messageID, err)
	}
	return nil
}

// MessageByID loads a single message, with its local sender's current
// username joined in as FromName (a remote sender's stored FromName
// is used as-is -- see Receive).
func (s *Store) MessageByID(id int64) (*Message, error) {
	row := s.db.QueryRow(
		`SELECT m.id, m.from_user_id, COALESCE(u.username, m.from_name) AS from_name, m.from_address,
		        m.to_user_id, m.to_name, m.to_address, m.subject, m.body, m.posted_at, m.read_at, m.sent_at, m.crash, m.email
		 FROM netmail_messages m LEFT JOIN users u ON u.id = m.from_user_id
		 WHERE m.id = ?`, id,
	)
	var m Message
	if err := row.Scan(&m.ID, &m.FromUserID, &m.FromName, &m.FromAddress,
		&m.ToUserID, &m.ToName, &m.ToAddress, &m.Subject, &m.Body, &m.PostedAt, &m.ReadAt, &m.SentAt, &m.Crash, &m.Email); err != nil {
		return nil, fmt.Errorf("netmail: load %d: %w", id, err)
	}
	return &m, nil
}

// Inbox returns userID's received netmail, newest first -- real
// netmail readers conventionally show the most recent message first,
// the opposite of an echo area's oldest-first message list.
func (s *Store) Inbox(userID int64) ([]Message, error) {
	rows, err := s.db.Query(
		`SELECT m.id, m.from_user_id, COALESCE(u.username, m.from_name) AS from_name, m.from_address,
		        m.to_user_id, m.to_name, m.to_address, m.subject, m.body, m.posted_at, m.read_at, m.sent_at, m.crash, m.email
		 FROM netmail_messages m LEFT JOIN users u ON u.id = m.from_user_id
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
			&m.ToUserID, &m.ToName, &m.ToAddress, &m.Subject, &m.Body, &m.PostedAt, &m.ReadAt, &m.SentAt, &m.Crash, &m.Email); err != nil {
			return nil, fmt.Errorf("netmail: scan inbox row: %w", err)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("netmail: inbox for user %d: %w", userID, err)
	}
	return msgs, nil
}

// Sent returns netmail userID has sent, newest first -- Inbox's
// counterpart, needed so a caller can find a message again after
// sending it (an FTN-addressed one has no local recipient to ever
// show it in an Inbox at all).
func (s *Store) Sent(userID int64) ([]Message, error) {
	rows, err := s.db.Query(
		`SELECT m.id, m.from_user_id, COALESCE(u.username, m.from_name) AS from_name, m.from_address,
		        m.to_user_id, m.to_name, m.to_address, m.subject, m.body, m.posted_at, m.read_at, m.sent_at, m.crash, m.email
		 FROM netmail_messages m LEFT JOIN users u ON u.id = m.from_user_id
		 WHERE m.from_user_id = ?
		 ORDER BY m.posted_at DESC, m.id DESC`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("netmail: sent for user %d: %w", userID, err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.FromUserID, &m.FromName, &m.FromAddress,
			&m.ToUserID, &m.ToName, &m.ToAddress, &m.Subject, &m.Body, &m.PostedAt, &m.ReadAt, &m.SentAt, &m.Crash, &m.Email); err != nil {
			return nil, fmt.Errorf("netmail: scan sent row: %w", err)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("netmail: sent for user %d: %w", userID, err)
	}
	return msgs, nil
}

// UnresolvedInbox returns inbound netmail whose recipient name never
// resolved to any local user account, most recent first -- a reply
// from an Areafix/Filefix robot, for instance, addressed back to
// whatever name this system used as its own request's From (see
// internal/tosser's RequestEchoAreaChanges/RequestFileAreaChanges),
// which isn't a real BBS username. Inbox alone would never surface
// this to anyone (it's filtered to one specific recipient), so the
// sysop's own netmail view merges this in too -- otherwise a reply
// like that is stored (never silently discarded) but effectively
// invisible in the BBS itself.
func (s *Store) UnresolvedInbox(limit int) ([]Message, error) {
	rows, err := s.db.Query(
		`SELECT m.id, m.from_user_id, COALESCE(u.username, m.from_name) AS from_name, m.from_address,
		        m.to_user_id, m.to_name, m.to_address, m.subject, m.body, m.posted_at, m.read_at, m.sent_at, m.crash, m.email
		 FROM netmail_messages m LEFT JOIN users u ON u.id = m.from_user_id
		 WHERE m.to_user_id IS NULL AND m.to_address = '' AND m.email = ''
		 ORDER BY m.posted_at DESC, m.id DESC
		 LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("netmail: unresolved inbox: %w", err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.FromUserID, &m.FromName, &m.FromAddress,
			&m.ToUserID, &m.ToName, &m.ToAddress, &m.Subject, &m.Body, &m.PostedAt, &m.ReadAt, &m.SentAt, &m.Crash, &m.Email); err != nil {
			return nil, fmt.Errorf("netmail: scan unresolved inbox row: %w", err)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("netmail: unresolved inbox: %w", err)
	}
	return msgs, nil
}

// CountUnresolvedInbox reports how many messages UnresolvedInbox
// would return with no limit -- for a dashboard badge, where the
// admin list's own display cap shouldn't understate the real number.
func (s *Store) CountUnresolvedInbox() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM netmail_messages WHERE to_user_id IS NULL AND to_address = '' AND email = ''`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("netmail: count unresolved inbox: %w", err)
	}
	return n, nil
}

// InboxFromAddress returns netmail from fromAddress, most recent
// first (at most limit messages), regardless of whether its recipient
// name resolved to a local user -- an inbound reply from an automated
// robot (e.g. an Areafix/Filefix subscription-list reply, addressed
// to whatever name we sent as the request's own From) commonly
// doesn't resolve to any real account, so it would never appear in
// any user's normal Inbox; this is how internal/web's Areafix UI
// finds it instead.
func (s *Store) InboxFromAddress(fromAddress string, limit int) ([]Message, error) {
	rows, err := s.db.Query(
		`SELECT m.id, m.from_user_id, COALESCE(u.username, m.from_name) AS from_name, m.from_address,
		        m.to_user_id, m.to_name, m.to_address, m.subject, m.body, m.posted_at, m.read_at, m.sent_at, m.crash, m.email
		 FROM netmail_messages m LEFT JOIN users u ON u.id = m.from_user_id
		 WHERE m.from_address = ?
		 ORDER BY m.posted_at DESC, m.id DESC
		 LIMIT ?`, fromAddress, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("netmail: inbox from %s: %w", fromAddress, err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.FromUserID, &m.FromName, &m.FromAddress,
			&m.ToUserID, &m.ToName, &m.ToAddress, &m.Subject, &m.Body, &m.PostedAt, &m.ReadAt, &m.SentAt, &m.Crash, &m.Email); err != nil {
			return nil, fmt.Errorf("netmail: scan inbox-from-address row: %w", err)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("netmail: inbox from %s: %w", fromAddress, err)
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

// Delete permanently removes a netmail message -- so an inbox (or a
// sysop's merged view of unresolved system replies, see
// UnresolvedInbox) doesn't just accumulate forever with no way to
// clear it out. Deleting a message that's still pending outbound
// (queued but not yet sent) simply drops it instead of sending it.
// Absent is not an error.
func (s *Store) Delete(messageID int64) error {
	if _, err := s.db.Exec(`DELETE FROM netmail_messages WHERE id = ?`, messageID); err != nil {
		return fmt.Errorf("netmail: delete %d: %w", messageID, err)
	}
	return nil
}

// Neighbors returns the IDs of the messages immediately before and
// after id within userID's own inbox, in the same newest-first order
// Inbox uses -- see message.Store.Neighbors, which this mirrors (for
// the web BBS portal reader's Prev/Next navigation) including why the
// (posted_at, id) comparisons run entirely in SQL against id's own
// stored row rather than a Go-side time.Time parameter, just with
// Inbox's reversed sort direction: "before" in list order here means
// newer (larger posted_at/id), "after" means older.
func (s *Store) Neighbors(userID, id int64) (before, after *int64, err error) {
	var b int64
	switch err := s.db.QueryRow(
		`SELECT id FROM netmail_messages WHERE to_user_id = ?
		 AND (posted_at, id) > (SELECT posted_at, id FROM netmail_messages WHERE id = ?)
		 ORDER BY posted_at ASC, id ASC LIMIT 1`,
		userID, id,
	).Scan(&b); {
	case err == nil:
		before = &b
	case errors.Is(err, sql.ErrNoRows):
	default:
		return nil, nil, fmt.Errorf("netmail: neighbors before %d: %w", id, err)
	}

	var a int64
	switch err := s.db.QueryRow(
		`SELECT id FROM netmail_messages WHERE to_user_id = ?
		 AND (posted_at, id) < (SELECT posted_at, id FROM netmail_messages WHERE id = ?)
		 ORDER BY posted_at DESC, id DESC LIMIT 1`,
		userID, id,
	).Scan(&a); {
	case err == nil:
		after = &a
	case errors.Is(err, sql.ErrNoRows):
	default:
		return nil, nil, fmt.Errorf("netmail: neighbors after %d: %w", id, err)
	}
	return before, after, nil
}

// SentNeighbors is Neighbors' counterpart for Sent, scoped by
// from_user_id instead of to_user_id.
func (s *Store) SentNeighbors(userID, id int64) (before, after *int64, err error) {
	var b int64
	switch err := s.db.QueryRow(
		`SELECT id FROM netmail_messages WHERE from_user_id = ?
		 AND (posted_at, id) > (SELECT posted_at, id FROM netmail_messages WHERE id = ?)
		 ORDER BY posted_at ASC, id ASC LIMIT 1`,
		userID, id,
	).Scan(&b); {
	case err == nil:
		before = &b
	case errors.Is(err, sql.ErrNoRows):
	default:
		return nil, nil, fmt.Errorf("netmail: sent neighbors before %d: %w", id, err)
	}

	var a int64
	switch err := s.db.QueryRow(
		`SELECT id FROM netmail_messages WHERE from_user_id = ?
		 AND (posted_at, id) < (SELECT posted_at, id FROM netmail_messages WHERE id = ?)
		 ORDER BY posted_at DESC, id DESC LIMIT 1`,
		userID, id,
	).Scan(&a); {
	case err == nil:
		after = &a
	case errors.Is(err, sql.ErrNoRows):
	default:
		return nil, nil, fmt.Errorf("netmail: sent neighbors after %d: %w", id, err)
	}
	return before, after, nil
}

// IsFTNAddress is a cheap heuristic -- not full FTN validation -- for
// telling "the caller typed a local username" apart from "the caller
// typed a FidoNet routing address" (zone:net/node[.point], e.g.
// "1:234/56" or "1:234/56.1") when resolving a netmail recipient.
// Shared by internal/bbs's own Netmail compose flow and internal/web's
// BBS-portal equivalent, so the two don't drift.
func IsFTNAddress(s string) bool {
	zoneRest := strings.SplitN(s, ":", 2)
	if len(zoneRest) != 2 {
		return false
	}
	if _, err := strconv.Atoi(zoneRest[0]); err != nil {
		return false
	}
	netNode := strings.SplitN(zoneRest[1], "/", 2)
	if len(netNode) != 2 {
		return false
	}
	if _, err := strconv.Atoi(netNode[0]); err != nil {
		return false
	}
	nodePoint := strings.SplitN(netNode[1], ".", 2)
	if _, err := strconv.Atoi(nodePoint[0]); err != nil {
		return false
	}
	if len(nodePoint) == 2 {
		if _, err := strconv.Atoi(nodePoint[1]); err != nil {
			return false
		}
	}
	return true
}
