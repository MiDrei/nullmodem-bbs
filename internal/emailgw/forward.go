package emailgw

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"mime"
	"mime/quotedprintable"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-smtp"

	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/i18n"
	"github.com/midrei/nullmodem-bbs/internal/netmail"
	"github.com/midrei/nullmodem-bbs/internal/user"
	"github.com/midrei/nullmodem-bbs/internal/version"
	"github.com/midrei/nullmodem-kit/ansi"
)

// A caller may have their netmail -- all of it: FTN, local and the
// gateway's own mail -- also sent to an address of theirs, and marked
// read when it went. The address counts once it's confirmed with a
// code mailed to it, so nobody can have the board mail a stranger.

// Forward is a caller's netmail forwarding.
type Forward struct {
	Address  string
	Verified bool // forwarding is on
	// Pending: a code went to Address and wasn't entered yet.
	Pending  bool
	MarkRead bool
}

const (
	// CodeValid is how long a confirmation code may be entered;
	// codeTries how often it may be guessed; codeGap the least time
	// between two code mails.
	CodeValid = 30 * time.Minute
	codeTries = 5
	codeGap   = time.Minute
	// forwardsPerDay caps what one caller's forwarding sends a day;
	// the rest waits for the next day.
	forwardsPerDay = 100
)

var (
	ErrForwardAddress   = errors.New("not an email address")
	ErrForwardOwnDomain = errors.New("an address of the gateway's own domains")
	ErrCodeTooSoon      = errors.New("a code was sent less than a minute ago")
	ErrNoCode           = errors.New("no code was asked for")
	ErrCodeExpired      = errors.New("the code expired")
	ErrCodeWrong        = errors.New("wrong code")
)

// Forwards keeps the callers' forwarding.
type Forwards struct{ db *sql.DB }

// NewForwards wraps an open database.
func NewForwards(db *sql.DB) *Forwards { return &Forwards{db: db} }

// Get is userID's forwarding, nil if none is set.
func (f *Forwards) Get(userID int64) (*Forward, error) {
	var fw Forward
	var hash string
	err := f.db.QueryRow(`SELECT address, verified, mark_read, code_hash FROM netmail_forward WHERE user_id = ?`, userID).
		Scan(&fw.Address, &fw.Verified, &fw.MarkRead, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("emailgw: forwarding of %d: %w", userID, err)
	}
	fw.Pending = hash != ""
	return &fw, nil
}

func codeHash(userID int64, code string) string {
	h := sha256.Sum256([]byte(strconv.FormatInt(userID, 10) + "|" + code))
	return hex.EncodeToString(h[:])
}

// CheckForwardAddress says what's wrong with addr as u's forwarding
// address under cfg, nil if nothing.
func CheckForwardAddress(cfg config.EmailConfig, addr string) error {
	if !netmail.IsEmailAddress(addr) {
		return ErrForwardAddress
	}
	at := strings.LastIndex(addr, "@")
	domain := strings.ToLower(addr[at+1:])
	for _, d := range cfg.Domains() {
		if domain == d {
			return ErrForwardOwnDomain
		}
	}
	return nil
}

// Request mails a confirmation code to addr for u; once it's entered
// (Confirm), u's netmail goes there. Forwarding to an earlier address
// stops until then.
func (f *Forwards) Request(cfg config.EmailConfig, u *user.User, addr, lang, bbsName string, now time.Time) error {
	addr = strings.TrimSpace(addr)
	if err := CheckForwardAddress(cfg, addr); err != nil {
		return err
	}
	var sent int64
	err := f.db.QueryRow(`SELECT code_sent FROM netmail_forward WHERE user_id = ?`, u.ID).Scan(&sent)
	if err == nil && now.Sub(time.Unix(sent, 0)) < codeGap {
		return ErrCodeTooSoon
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return fmt.Errorf("emailgw: making a code: %w", err)
	}
	code := fmt.Sprintf("%06d", n.Int64())
	from := Address(cfg, u.Username)
	text := i18n.T(lang, "email.forward_code_body", "USER", u.Username, "BBS", bbsName, "CODE", code, "MINUTES", int(CodeValid/time.Minute))
	raw := plainMail(cfg, bbsName, bbsName, from, addr, i18n.T(lang, "email.forward_code_subject", "BBS", bbsName), text, "", newMessageID(cfg.Domain), now)
	if err := sendMail(cfg, from, addr, raw); err != nil {
		return err
	}
	if _, err := f.db.Exec(`INSERT INTO netmail_forward (user_id, address, verified, code_hash, code_sent, code_tries)
		VALUES (?, ?, 0, ?, ?, 0)
		ON CONFLICT(user_id) DO UPDATE SET address = excluded.address, verified = 0,
			code_hash = excluded.code_hash, code_sent = excluded.code_sent, code_tries = 0`,
		u.ID, addr, codeHash(u.ID, code), now.Unix()); err != nil {
		return fmt.Errorf("emailgw: saving the forwarding of %d: %w", u.ID, err)
	}
	return nil
}

// Confirm checks the code userID got; right, their netmail from now on
// goes to the address.
func (f *Forwards) Confirm(userID int64, code string, now time.Time) error {
	var hash string
	var sent int64
	var tries int
	err := f.db.QueryRow(`SELECT code_hash, code_sent, code_tries FROM netmail_forward WHERE user_id = ?`, userID).Scan(&hash, &sent, &tries)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && hash == "") {
		return ErrNoCode
	}
	if err != nil {
		return fmt.Errorf("emailgw: forwarding of %d: %w", userID, err)
	}
	if tries >= codeTries || now.Sub(time.Unix(sent, 0)) > CodeValid {
		return ErrCodeExpired
	}
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if subtle.ConstantTimeCompare([]byte(codeHash(userID, code)), []byte(hash)) != 1 {
		f.db.Exec(`UPDATE netmail_forward SET code_tries = code_tries + 1 WHERE user_id = ?`, userID)
		return ErrCodeWrong
	}
	if _, err := f.db.Exec(`UPDATE netmail_forward SET verified = 1, code_hash = '', code_tries = 0,
			from_id = (SELECT COALESCE(MAX(id), 0) FROM netmail_messages) WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("emailgw: confirming the forwarding of %d: %w", userID, err)
	}
	return nil
}

// SetMarkRead sets whether forwarded netmail counts as read.
func (f *Forwards) SetMarkRead(userID int64, on bool) error {
	if _, err := f.db.Exec(`UPDATE netmail_forward SET mark_read = ? WHERE user_id = ?`, on, userID); err != nil {
		return fmt.Errorf("emailgw: forwarding of %d: %w", userID, err)
	}
	return nil
}

// Remove ends userID's forwarding.
func (f *Forwards) Remove(userID int64) error {
	if _, err := f.db.Exec(`DELETE FROM netmail_forward WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("emailgw: removing the forwarding of %d: %w", userID, err)
	}
	return nil
}

// sendMail sends one mail over its own SMTP connection.
func sendMail(cfg config.EmailConfig, from, to string, raw []byte) error {
	c, err := dialSMTP(cfg.SMTP)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.SendMail(from, []string{to}, bytes.NewReader(raw)); err != nil {
		return err
	}
	return c.Quit()
}

// plainMail writes a UTF-8 text mail from the gateway.
func plainMail(cfg config.EmailConfig, bbsName, fromName, from, to, subject, text, replyTo, msgID string, now time.Time) []byte {
	var b bytes.Buffer
	h := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	h("From", mime.QEncoding.Encode("utf-8", fromName)+" <"+from+">")
	h("To", "<"+to+">")
	if replyTo != "" {
		h("Reply-To", "<"+replyTo+">")
	}
	h("Subject", mime.QEncoding.Encode("utf-8", subject))
	h("Date", now.Format(time.RFC1123Z))
	h("Message-ID", msgID)
	h("MIME-Version", "1.0")
	h("Content-Type", "text/plain; charset=utf-8")
	h("Content-Transfer-Encoding", "quoted-printable")
	h("X-Mailer", mime.QEncoding.Encode("utf-8", bbsName)+" ("+version.Version+")")
	h(loopHeader, strings.ToLower(cfg.Domain))
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	qp.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")))
	qp.Close()
	return b.Bytes()
}

// forwardJob is a netmail to forward.
type forwardJob struct {
	id, userID int64
	address    string
	markRead   bool
	attempts   int
	firstTry   int64
}

// ForwardPending forwards the netmail that's due, one SMTP connection
// for all of it.
func (g *Gateway) ForwardPending(ctx context.Context, cfg config.EmailConfig, now time.Time) {
	rows, err := g.DB.Query(`SELECT m.id, f.user_id, f.address, f.mark_read, COALESCE(x.attempts, 0), COALESCE(x.first_try, 0)
		FROM netmail_messages m
		JOIN netmail_forward f ON f.user_id = m.to_user_id AND f.verified = 1 AND m.id > f.from_id
		LEFT JOIN netmail_forwarded x ON x.netmail_id = m.id
		WHERE x.netmail_id IS NULL OR (x.done = 0 AND x.next_try <= ?)
		ORDER BY m.id LIMIT 50`, now.Unix())
	if err != nil {
		g.logWarn("email gateway: forwarding: %v", err)
		return
	}
	var jobs []forwardJob
	for rows.Next() {
		var j forwardJob
		if rows.Scan(&j.id, &j.userID, &j.address, &j.markRead, &j.attempts, &j.firstTry) == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()
	if len(jobs) == 0 {
		return
	}
	var c *smtp.Client
	defer func() {
		if c != nil {
			c.Quit()
			c.Close()
		}
	}()
	for _, j := range jobs {
		if ctx.Err() != nil {
			return
		}
		u, err := g.Users.ByID(j.userID)
		if err != nil || !May(cfg, u) {
			g.forwardDone(j, now, false, "not allowed to use email (any more)")
			continue
		}
		var today int
		g.DB.QueryRow(`SELECT COUNT(*) FROM netmail_forwarded WHERE user_id = ? AND sent_at > ?`, j.userID, now.Add(-24*time.Hour).Unix()).Scan(&today)
		if today >= forwardsPerDay {
			continue // tomorrow
		}
		m, err := g.Netmail.MessageByID(j.id)
		if err != nil {
			g.forwardDone(j, now, false, err.Error())
			continue
		}
		if c == nil {
			if c, err = dialSMTP(cfg.SMTP); err != nil {
				g.update(func(st *Status) { st.LastSendError = err.Error() })
				for _, j := range jobs {
					g.forwardRetry(j, err, now)
				}
				return
			}
		}
		from := Address(cfg, u.Username)
		raw := g.composeForward(cfg, u, m, j.address, now)
		if err := c.SendMail(from, []string{j.address}, bytes.NewReader(raw)); err != nil {
			var se *smtp.SMTPError
			if errors.As(err, &se) && se.Code >= 500 && !smarthostRefused(se) {
				g.forwardDone(j, now, false, err.Error())
				g.logWarn("email gateway: netmail %d not forwarded to %s's address: %v", j.id, u.Username, err)
				continue
			}
			g.forwardRetry(j, err, now)
			continue
		}
		g.forwardDone(j, now, true, "")
		if j.markRead {
			if err := g.Netmail.MarkRead(j.id); err != nil {
				g.logWarn("email gateway: %v", err)
			}
		}
		g.update(func(st *Status) { st.LastSend, st.LastSendError = now, ""; st.Sent++ })
		g.logInfo("email gateway: netmail %d forwarded to %s's address", j.id, u.Username)
	}
}

func (g *Gateway) forwardDone(j forwardJob, now time.Time, sent bool, reason string) {
	var sentAt int64
	if sent {
		sentAt = now.Unix()
	}
	first := j.firstTry
	if first == 0 {
		first = now.Unix()
	}
	if _, err := g.DB.Exec(`INSERT INTO netmail_forwarded (netmail_id, user_id, done, attempts, first_try, sent_at, error)
		VALUES (?, ?, 1, ?, ?, ?, ?)
		ON CONFLICT(netmail_id) DO UPDATE SET done = 1, attempts = excluded.attempts, sent_at = excluded.sent_at, error = excluded.error`,
		j.id, j.userID, j.attempts+1, first, sentAt, reason); err != nil {
		g.logWarn("email gateway: forwarding: %v", err)
	}
}

func (g *Gateway) forwardRetry(j forwardJob, err error, now time.Time) {
	if j.firstTry != 0 && now.Sub(time.Unix(j.firstTry, 0)) > GiveUpAfter {
		g.forwardDone(j, now, false, err.Error())
		g.logWarn("email gateway: netmail %d not forwarded: %v", j.id, err)
		return
	}
	first := j.firstTry
	if first == 0 {
		first = now.Unix()
	}
	if _, e := g.DB.Exec(`INSERT INTO netmail_forwarded (netmail_id, user_id, attempts, first_try, next_try, error)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(netmail_id) DO UPDATE SET attempts = excluded.attempts, next_try = excluded.next_try, error = excluded.error`,
		j.id, j.userID, j.attempts+1, first, now.Add(retryDelay(j.attempts)).Unix(), err.Error()); e != nil {
		g.logWarn("email gateway: forwarding: %v", e)
	}
}

// composeForward writes netmail m as a mail to u's address to.
func (g *Gateway) composeForward(cfg config.EmailConfig, u *user.User, m *netmail.Message, to string, now time.Time) []byte {
	name := "NullModem BBS"
	if g.BBSName != nil && g.BBSName() != "" {
		name = g.BBSName()
	}
	lang := g.Lang(u)
	sender := ansi.DecodeCP437([]byte(m.FromName))
	who := sender
	switch {
	case m.Email != "":
		who += " <" + m.Email + ">"
	case m.FromAddress != "":
		who += " (" + m.FromAddress + ")"
	}
	text := i18n.T(lang, "email.forward_intro", "FROM", who, "BBS", name,
		"DATE", m.PostedAt.In(u.Location()).Format("2006-01-02 15:04")) + "\n\n" +
		PlainBody(m.Body) + "\n-- \n" + i18n.T(lang, "email.forward_footer", "BBS", name) + "\n"
	return plainMail(cfg, name, i18n.T(lang, "email.forward_from", "FROM", sender, "BBS", name), Address(cfg, u.Username), to,
		ansi.DecodeCP437([]byte(m.Subject)), text, m.Email, newMessageID(cfg.Domain), now)
}
