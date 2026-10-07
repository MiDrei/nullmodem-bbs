package netmail

import (
	"database/sql"
	"errors"
	"fmt"
	"git.maik.ch/nullmodem/bbs/internal/textclean"
	"net/mail"
	"strings"
	"time"
)

// Mail through the email gateway (internal/emailgw) is netmail too: a
// row with Email set, the address on the other side. One written here
// waits (sent_at NULL) until the gateway hands it to the SMTP server;
// one that came in sits in its recipient's inbox like any netmail.
// email_meta keeps what the mail needs besides: its Message-ID, the
// one it answers, and how sending it goes.

// ErrDuplicate is a mail that is here already (the same Message-ID for
// the same recipient) -- fetched twice from the mailbox.
var ErrDuplicate = errors.New("netmail: mail already received")

// IsEmailAddress reports whether s is an email address (name@example.com)
// -- not an FTN address and not "Name@zone:net/node".
func IsEmailAddress(s string) bool {
	s = strings.TrimSpace(s)
	at := strings.LastIndex(s, "@")
	if at <= 0 || at == len(s)-1 || strings.ContainsAny(s, " <>,;:/\"") {
		return false
	}
	domain := s[at+1:]
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s
}

// SendEmail stores a mail fromUserID writes to the address to. replyTo
// is the netmail it answers (0: none) -- a mail that came in, so this
// one goes out as its reply.
func (s *Store) SendEmail(fromUserID int64, fromAddress, to, subject, body string, replyTo int64) (*Message, error) {
	subject = textclean.Line(subject)
	to = strings.TrimSpace(to)
	if !IsEmailAddress(to) {
		return nil, fmt.Errorf("netmail: %q is not an email address", to)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("netmail: send email: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(
		`INSERT INTO netmail_messages (from_user_id, from_address, to_name, to_address, subject, body, email)
		 VALUES (?, ?, ?, '', ?, ?, ?)`,
		fromUserID, fromAddress, to, subject, body, to,
	)
	if err != nil {
		return nil, fmt.Errorf("netmail: send email: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("netmail: send email: %w", err)
	}
	var inReplyTo string
	if replyTo > 0 {
		tx.QueryRow(`SELECT message_id FROM email_meta WHERE netmail_id = ?`, replyTo).Scan(&inReplyTo)
	}
	if _, err := tx.Exec(`INSERT INTO email_meta (netmail_id, in_reply_to) VALUES (?, ?)`, id, inReplyTo); err != nil {
		return nil, fmt.Errorf("netmail: send email: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("netmail: send email: %w", err)
	}
	return s.MessageByID(id)
}

// ReceiveEmail stores a mail that came in for toUserID from the address
// from (fromName: their name, else the address). messageID and
// inReplyTo are its headers; a messageID already received for the same
// user is ErrDuplicate.
func (s *Store) ReceiveEmail(fromName, from string, toUserID int64, toName, subject, body string, postedAt time.Time, messageID, inReplyTo string) (*Message, error) {
	fromName, toName, subject = textclean.Line(fromName), textclean.Line(toName), textclean.Line(subject)
	if fromName == "" {
		fromName = from
	}
	if messageID != "" {
		var n int
		s.db.QueryRow(`SELECT COUNT(*) FROM email_meta e JOIN netmail_messages m ON m.id = e.netmail_id
			WHERE e.message_id = ? AND m.to_user_id = ?`, messageID, toUserID).Scan(&n)
		if n > 0 {
			return nil, ErrDuplicate
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("netmail: receive email: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(
		`INSERT INTO netmail_messages (from_user_id, from_name, from_address, to_user_id, to_name, to_address, subject, body, posted_at, sent_at, email)
		 VALUES (NULL, ?, '', ?, ?, '', ?, ?, ?, CURRENT_TIMESTAMP, ?)`,
		fromName, toUserID, toName, subject, body, postedAt.UTC(), from,
	)
	if err != nil {
		return nil, fmt.Errorf("netmail: receive email: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("netmail: receive email: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO email_meta (netmail_id, message_id, in_reply_to) VALUES (?, ?, ?)`, id, messageID, inReplyTo); err != nil {
		return nil, fmt.Errorf("netmail: receive email: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("netmail: receive email: %w", err)
	}
	return s.MessageByID(id)
}

// OutgoingEmail is a mail waiting to go out, with what sending it needs.
type OutgoingEmail struct {
	Message
	InReplyTo string
	Attempts  int
}

// PendingEmail returns the mail written here that is due to go out at
// now, oldest first: not sent, not given up on, its next try reached.
func (s *Store) PendingEmail(now time.Time) ([]OutgoingEmail, error) {
	rows, err := s.db.Query(
		`SELECT m.id, e.in_reply_to, e.attempts
		 FROM netmail_messages m JOIN email_meta e ON e.netmail_id = m.id
		 WHERE m.email != '' AND m.from_user_id IS NOT NULL AND m.sent_at IS NULL AND e.failed = 0 AND e.next_try <= ?
		 ORDER BY m.posted_at ASC, m.id ASC`, now.Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("netmail: pending email: %w", err)
	}
	type due struct {
		id        int64
		inReplyTo string
		attempts  int
	}
	var list []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.inReplyTo, &d.attempts); err != nil {
			rows.Close()
			return nil, fmt.Errorf("netmail: pending email: %w", err)
		}
		list = append(list, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("netmail: pending email: %w", err)
	}
	out := make([]OutgoingEmail, 0, len(list))
	for _, d := range list {
		m, err := s.MessageByID(d.id)
		if err != nil {
			return nil, err
		}
		out = append(out, OutgoingEmail{Message: *m, InReplyTo: d.inReplyTo, Attempts: d.attempts})
	}
	return out, nil
}

// EmailSent records that mail id went out as messageID.
func (s *Store) EmailSent(id int64, messageID string) error {
	if _, err := s.db.Exec(`UPDATE netmail_messages SET sent_at = CURRENT_TIMESTAMP WHERE id = ?`, id); err != nil {
		return fmt.Errorf("netmail: email %d sent: %w", id, err)
	}
	if _, err := s.db.Exec(`UPDATE email_meta SET message_id = ?, last_error = '' WHERE netmail_id = ?`, messageID, id); err != nil {
		return fmt.Errorf("netmail: email %d sent: %w", id, err)
	}
	return nil
}

// EmailRetry records a failed try of mail id; the next is at next.
func (s *Store) EmailRetry(id int64, reason string, next time.Time) error {
	if _, err := s.db.Exec(`UPDATE email_meta SET attempts = attempts + 1, last_error = ?, next_try = ? WHERE netmail_id = ?`,
		reason, next.Unix(), id); err != nil {
		return fmt.Errorf("netmail: email %d retry: %w", id, err)
	}
	return nil
}

// EmailFailed gives up on mail id.
func (s *Store) EmailFailed(id int64, reason string) error {
	if _, err := s.db.Exec(`UPDATE email_meta SET attempts = attempts + 1, last_error = ?, failed = 1 WHERE netmail_id = ?`,
		reason, id); err != nil {
		return fmt.Errorf("netmail: email %d failed: %w", id, err)
	}
	return nil
}

// EmailsSince counts the mail userID wrote since t (for the daily limit).
func (s *Store) EmailsSince(userID int64, t time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM netmail_messages
		WHERE from_user_id = ? AND email != '' AND substr(posted_at, 1, 19) >= ?`,
		userID, t.UTC().Format("2006-01-02 15:04:05")).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("netmail: counting emails: %w", err)
	}
	return n, nil
}

// EmailState is how a mail written here is going.
type EmailState struct {
	Attempts  int
	LastError string
	Failed    bool
}

// EmailStateOf returns mail id's sending state.
func (s *Store) EmailStateOf(id int64) (EmailState, error) {
	var st EmailState
	err := s.db.QueryRow(`SELECT attempts, last_error, failed FROM email_meta WHERE netmail_id = ?`, id).
		Scan(&st.Attempts, &st.LastError, &st.Failed)
	if errors.Is(err, sql.ErrNoRows) {
		return st, nil
	}
	return st, err
}

// EmailQueue is the outgoing mail not sent yet: how many, the oldest's
// time, and how many were given up on in the last day.
func (s *Store) EmailQueue(now time.Time) (waiting int, oldest time.Time, failed int, err error) {
	var first sql.NullString
	err = s.db.QueryRow(`SELECT COUNT(*), MIN(substr(m.posted_at, 1, 19)) FROM netmail_messages m JOIN email_meta e ON e.netmail_id = m.id
		WHERE m.email != '' AND m.from_user_id IS NOT NULL AND m.sent_at IS NULL AND e.failed = 0`).Scan(&waiting, &first)
	if err != nil {
		return 0, time.Time{}, 0, fmt.Errorf("netmail: email queue: %w", err)
	}
	if first.Valid && len(first.String) >= 19 {
		oldest, _ = time.Parse("2006-01-02 15:04:05", first.String[:19])
	}
	err = s.db.QueryRow(`SELECT COUNT(*) FROM netmail_messages m JOIN email_meta e ON e.netmail_id = m.id
		WHERE e.failed = 1 AND substr(m.posted_at, 1, 19) >= ?`, now.Add(-24*time.Hour).UTC().Format("2006-01-02 15:04:05")).Scan(&failed)
	if err != nil {
		return 0, time.Time{}, 0, fmt.Errorf("netmail: email queue: %w", err)
	}
	return waiting, oldest, failed, nil
}
