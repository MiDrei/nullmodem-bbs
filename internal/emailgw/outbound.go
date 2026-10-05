package emailgw

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/i18n"
	"git.maik.ch/nullmodem/bbs/internal/netmail"
	"git.maik.ch/nullmodem/bbs/internal/user"
	"git.maik.ch/nullmodem/bbs/internal/version"
	"git.maik.ch/nullmodem/kit/ansi"
)

// GiveUpAfter is how long a mail is retried before it's returned to
// its writer.
const GiveUpAfter = 24 * time.Hour

// retryDelay is the wait before try n+1 (n tries failed so far).
func retryDelay(n int) time.Duration {
	steps := []time.Duration{2 * time.Minute, 10 * time.Minute, 30 * time.Minute, time.Hour}
	if n < len(steps) {
		return steps[n]
	}
	return 2 * time.Hour
}

// SendPending sends the mail that's due, one SMTP connection for all.
func (g *Gateway) SendPending(ctx context.Context, cfg config.EmailConfig, now time.Time) {
	due, err := g.Netmail.PendingEmail(now)
	if err != nil {
		g.logWarn("email gateway: %v", err)
		return
	}
	if len(due) == 0 {
		return
	}
	c, err := dialSMTP(cfg.SMTP)
	if err != nil {
		g.update(func(st *Status) { st.LastSendError = err.Error() })
		g.logWarn("email gateway: SMTP: %v", err)
		for _, m := range due {
			g.retry(m, err, now)
		}
		return
	}
	defer c.Close()
	for _, m := range due {
		if ctx.Err() != nil {
			return
		}
		g.sendOne(c, cfg, m, now)
	}
	c.Quit()
}

func (g *Gateway) sendOne(c *smtp.Client, cfg config.EmailConfig, m netmail.OutgoingEmail, now time.Time) {
	u, err := g.Users.ByID(m.FromUserID.Int64)
	if err != nil || !May(cfg, u) {
		// The writer was deleted, lost the right, or the gateway changed.
		g.giveUp(m, u, "not allowed to send email (any more)")
		return
	}
	from := Address(cfg, u.Username)
	msgID := newMessageID(cfg.Domain)
	raw := g.compose(cfg, u, m, msgID, now)
	err = c.SendMail(from, []string{m.Email}, bytes.NewReader(raw))
	if err != nil {
		var se *smtp.SMTPError
		if errors.As(err, &se) && se.Code >= 500 {
			g.giveUp(m, u, err.Error())
			return
		}
		g.update(func(st *Status) { st.LastSendError = err.Error() })
		g.retry(m, err, now)
		return
	}
	if err := g.Netmail.EmailSent(m.ID, msgID); err != nil {
		g.logWarn("email gateway: %v", err)
	}
	g.update(func(st *Status) { st.LastSend, st.LastSendError = now, ""; st.Sent++ })
	g.logInfo("email from %s to %s sent: %q", from, m.Email, m.Subject)
}

func (g *Gateway) retry(m netmail.OutgoingEmail, err error, now time.Time) {
	if now.Sub(m.PostedAt) > GiveUpAfter {
		u, _ := g.Users.ByID(m.FromUserID.Int64)
		g.giveUp(m, u, err.Error())
		return
	}
	if e := g.Netmail.EmailRetry(m.ID, err.Error(), now.Add(retryDelay(m.Attempts))); e != nil {
		g.logWarn("email gateway: %v", e)
	}
}

// giveUp stops trying mail m and tells its writer (u, may be nil) why.
func (g *Gateway) giveUp(m netmail.OutgoingEmail, u *user.User, reason string) {
	if err := g.Netmail.EmailFailed(m.ID, reason); err != nil {
		g.logWarn("email gateway: %v", err)
	}
	g.logWarn("email to %s (%q) not delivered: %s", m.Email, m.Subject, reason)
	if u == nil {
		return
	}
	lang := g.Lang(u)
	subject := ansi.DecodeCP437([]byte(m.Subject))
	body := i18n.T(lang, "email.bounce_body", "TO", m.Email, "SUBJECT", subject, "REASON", reason)
	if _, err := g.Netmail.Receive(i18n.T(lang, "common.email_gateway"), "", u.ID, u.Username, "",
		string(ansi.EncodeCP437(i18n.T(lang, "email.bounce_subject", "SUBJECT", subject))),
		string(ansi.EncodeCP437(body)), time.Now(), false); err != nil {
		g.logWarn("email gateway: telling %s: %v", u.Username, err)
	}
}

// compose writes mail m as an RFC 5322 message.
func (g *Gateway) compose(cfg config.EmailConfig, u *user.User, m netmail.OutgoingEmail, msgID string, now time.Time) []byte {
	var b bytes.Buffer
	h := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	from := Address(cfg, u.Username)
	h("From", mime.QEncoding.Encode("utf-8", u.Username)+" <"+from+">")
	h("To", "<"+m.Email+">")
	h("Subject", mime.QEncoding.Encode("utf-8", ansi.DecodeCP437([]byte(m.Subject))))
	h("Date", now.Format(time.RFC1123Z))
	h("Message-ID", msgID)
	if m.InReplyTo != "" {
		h("In-Reply-To", m.InReplyTo)
		h("References", m.InReplyTo)
	}
	h("MIME-Version", "1.0")
	h("Content-Type", "text/plain; charset=utf-8")
	h("Content-Transfer-Encoding", "quoted-printable")
	name := "NullModem BBS"
	if g.BBSName != nil && g.BBSName() != "" {
		name = g.BBSName()
	}
	h("X-Mailer", mime.QEncoding.Encode("utf-8", name)+" ("+version.Version+")")
	h(loopHeader, strings.ToLower(cfg.Domain))
	b.WriteString("\r\n")
	// Who wrote it, and where: the recipient may never have heard of
	// the board (the text is the sysop's to change, language editor).
	text := i18n.T(g.Lang(u), "email.sent_by", "USER", u.Username, "BBS", name) + "\n\n" + PlainBody(m.Body)
	qp := quotedprintable.NewWriter(&b)
	qp.Write([]byte(strings.ReplaceAll(text, "\n", "\r\n")))
	qp.Close()
	return b.Bytes()
}

// loopHeader marks mail the gateway sent, so it never takes it back in.
const loopHeader = "X-NullModem-Gateway"

// PlainBody is a stored (CP437) netmail body as mail text: UTF-8,
// without kludge lines, a final newline.
func PlainBody(body string) string {
	var keep []string
	for _, l := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(l, "\x01") || strings.HasPrefix(l, "SEEN-BY:") {
			continue
		}
		keep = append(keep, strings.TrimRight(ansi.DecodeCP437([]byte(l)), " \r"))
	}
	return strings.TrimRight(strings.Join(keep, "\n"), "\n") + "\n"
}

func newMessageID(domain string) string {
	var r [12]byte
	rand.Read(r[:])
	return "<nm." + strconv.FormatInt(time.Now().Unix(), 36) + "." + hex.EncodeToString(r[:]) + "@" + strings.ToLower(domain) + ">"
}

func serverAddr(s config.MailServer, def int) string {
	port := s.Port
	if port == 0 {
		port = def
	}
	return net.JoinHostPort(s.Host, strconv.Itoa(port))
}

// dialSMTP connects and logs in to the SMTP server.
func dialSMTP(s config.MailServer) (*smtp.Client, error) {
	if s.Host == "" {
		return nil, errors.New("no SMTP server set")
	}
	var c *smtp.Client
	var err error
	tc := &tls.Config{ServerName: s.Host}
	switch s.Security {
	case "starttls":
		c, err = smtp.DialStartTLS(serverAddr(s, 587), tc)
	case "none":
		c, err = smtp.Dial(serverAddr(s, 25))
	default:
		c, err = smtp.DialTLS(serverAddr(s, 465), tc)
	}
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", s.Host, err)
	}
	c.CommandTimeout = time.Minute
	c.SubmissionTimeout = 2 * time.Minute
	if s.User != "" {
		if err := c.Auth(sasl.NewPlainClient("", s.User, s.Password)); err != nil {
			c.Close()
			return nil, fmt.Errorf("logging in to %s: %w", s.Host, err)
		}
	}
	return c, nil
}
