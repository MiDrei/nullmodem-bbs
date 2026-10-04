package emailgw

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	_ "github.com/emersion/go-message/charset" // ISO-8859-1, Windows-1252 & co. in mail
	"github.com/emersion/go-message/mail"

	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/i18n"
	"git.maik.ch/nullmodem/bbs/internal/netmail"
	"git.maik.ch/nullmodem/bbs/internal/textfmt"
	"git.maik.ch/nullmodem/bbs/internal/user"
	"git.maik.ch/nullmodem/kit/ansi"
)

// Limits on what comes in: bigger mail arrives as its headers and a
// note; a longer text is cut; more mail than perRound waits for the
// next round.
const (
	maxMailSize = 5 << 20
	maxText     = 64 << 10
	perRound    = 50
)

// dialIMAP connects and logs in to the mailbox's server.
func dialIMAP(s config.MailServer) (*client.Client, error) {
	if s.Host == "" {
		return nil, errors.New("no IMAP server set")
	}
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	tc := &tls.Config{ServerName: s.Host}
	var c *client.Client
	var err error
	switch s.Security {
	case "starttls":
		if c, err = client.DialWithDialer(dialer, serverAddr(s, 143)); err == nil {
			if err = c.StartTLS(tc); err != nil {
				c.Logout()
			}
		}
	case "none":
		c, err = client.DialWithDialer(dialer, serverAddr(s, 143))
	default:
		c, err = client.DialWithDialerTLS(dialer, serverAddr(s, 993), tc)
	}
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", s.Host, err)
	}
	c.Timeout = 2 * time.Minute
	if err := c.Login(s.User, s.Password); err != nil {
		c.Logout()
		return nil, fmt.Errorf("logging in to %s: %w", s.Host, err)
	}
	return c, nil
}

func folder(s config.MailServer) string {
	if s.Folder == "" {
		return "INBOX"
	}
	return s.Folder
}

// Test logs in to both servers (and opens the IMAP folder).
func Test(ctx context.Context, cfg config.EmailConfig) error {
	c, err := dialIMAP(cfg.IMAP)
	if err != nil {
		return fmt.Errorf("IMAP: %w", err)
	}
	_, err = c.Select(folder(cfg.IMAP), true)
	c.Logout()
	if err != nil {
		return fmt.Errorf("IMAP: opening %s: %w", folder(cfg.IMAP), err)
	}
	s, err := dialSMTP(cfg.SMTP)
	if err != nil {
		return fmt.Errorf("SMTP: %w", err)
	}
	s.Quit()
	return nil
}

// Fetch takes the unread mail out of the mailbox: each becomes netmail
// to the callers it's addressed to, then is marked read (or deleted).
func (g *Gateway) Fetch(ctx context.Context, cfg config.EmailConfig) {
	n, err := g.fetch(ctx, cfg)
	now := time.Now()
	if err != nil {
		g.logWarn("email gateway: fetching mail: %v", err)
		g.update(func(st *Status) { st.LastFetchError = err.Error() })
		return
	}
	g.update(func(st *Status) { st.LastFetch, st.LastFetchError = now, "" })
	if n > 0 {
		g.logInfo("email gateway: %d mail(s) fetched", n)
	}
}

func (g *Gateway) fetch(ctx context.Context, cfg config.EmailConfig) (int, error) {
	c, err := dialIMAP(cfg.IMAP)
	if err != nil {
		return 0, err
	}
	defer c.Logout()
	if _, err := c.Select(folder(cfg.IMAP), false); err != nil {
		return 0, fmt.Errorf("opening %s: %w", folder(cfg.IMAP), err)
	}
	crit := imap.NewSearchCriteria()
	crit.WithoutFlags = []string{imap.SeenFlag, imap.DeletedFlag}
	uids, err := c.UidSearch(crit)
	if err != nil {
		return 0, fmt.Errorf("searching: %w", err)
	}
	if len(uids) > perRound {
		uids = uids[:perRound]
	}
	if len(uids) == 0 {
		return 0, nil
	}

	// Sizes first: a huge mail comes as its headers only.
	sizes := map[uint32]uint32{}
	set := new(imap.SeqSet)
	set.AddNum(uids...)
	ch := make(chan *imap.Message, 16)
	done := make(chan error, 1)
	go func() { done <- c.UidFetch(set, []imap.FetchItem{imap.FetchUid, imap.FetchRFC822Size}, ch) }()
	for m := range ch {
		sizes[m.Uid] = m.Size
	}
	if err := <-done; err != nil {
		return 0, fmt.Errorf("fetching sizes: %w", err)
	}

	handled := new(imap.SeqSet)
	count := 0
	for _, uid := range uids {
		if ctx.Err() != nil {
			break
		}
		section := &imap.BodySectionName{Peek: true}
		tooBig := sizes[uid] > maxMailSize
		if tooBig {
			section.Specifier = imap.HeaderSpecifier
		}
		one := new(imap.SeqSet)
		one.AddNum(uid)
		ch := make(chan *imap.Message, 1)
		done := make(chan error, 1)
		go func() { done <- c.UidFetch(one, []imap.FetchItem{imap.FetchUid, section.FetchItem()}, ch) }()
		var raw []byte
		for m := range ch {
			if r := m.GetBody(section); r != nil {
				raw, _ = io.ReadAll(r)
			}
		}
		if err := <-done; err != nil {
			return count, fmt.Errorf("fetching mail %d: %w", uid, err)
		}
		if err := g.take(cfg, raw, tooBig, sizes[uid]); err != nil {
			// Not taken (the database): left unread for the next round.
			g.logWarn("email gateway: mail %d: %v", uid, err)
			continue
		}
		handled.AddNum(uid)
		count++
	}
	if handled.Empty() {
		return count, nil
	}
	flags := []interface{}{imap.SeenFlag}
	if cfg.DeleteFetched {
		flags = append(flags, imap.DeletedFlag)
	}
	if err := c.UidStore(handled, imap.FormatFlagsOp(imap.AddFlags, true), flags, nil); err != nil {
		return count, fmt.Errorf("marking fetched mail: %w", err)
	}
	if cfg.DeleteFetched {
		if err := c.Expunge(nil); err != nil {
			return count, fmt.Errorf("deleting fetched mail: %w", err)
		}
	}
	return count, nil
}

// Incoming is a mail as the gateway reads it.
type Incoming struct {
	FromName, From string
	To             []string // every address it was sent to, lower case
	Subject        string
	Date           time.Time
	MessageID      string
	InReplyTo      string
	Text           string // UTF-8
	Attachments    []string
	Spam           bool
	Ours           bool // sent by this gateway (a loop)
}

// Parse reads a raw mail. headersOnly: raw is just its header.
func Parse(raw []byte, domain string) (*Incoming, error) {
	r, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil && r == nil {
		return nil, fmt.Errorf("reading mail: %w", err)
	}
	h := r.Header
	in := &Incoming{}
	if from, err := h.AddressList("From"); err == nil && len(from) > 0 {
		in.FromName, in.From = from[0].Name, strings.ToLower(from[0].Address)
	}
	if rt, err := h.AddressList("Reply-To"); err == nil && len(rt) > 0 && rt[0].Address != "" {
		// Answers go where the writer wants them.
		in.From = strings.ToLower(rt[0].Address)
	}
	in.Subject, _ = h.Subject()
	in.Date, _ = h.Date()
	if in.Date.IsZero() {
		in.Date = time.Now()
	}
	if id, err := h.MessageID(); err == nil && id != "" {
		in.MessageID = "<" + id + ">"
	}
	if ids, err := h.MsgIDList("In-Reply-To"); err == nil && len(ids) > 0 {
		in.InReplyTo = "<" + ids[0] + ">"
	}
	seen := map[string]bool{}
	for _, k := range []string{"Delivered-To", "X-Original-To", "Envelope-To", "X-Envelope-To", "To", "Cc"} {
		for _, v := range h.Values(k) {
			list, err := mail.ParseAddressList(v)
			if err != nil {
				// A bare address (Delivered-To) parses as itself.
				list = []*mail.Address{{Address: strings.Trim(strings.TrimSpace(v), "<>")}}
			}
			for _, a := range list {
				addr := strings.ToLower(a.Address)
				if addr != "" && !seen[addr] {
					seen[addr] = true
					in.To = append(in.To, addr)
				}
			}
		}
	}
	flag := strings.ToLower(strings.TrimSpace(h.Get("X-Spam-Flag")))
	status := strings.ToLower(strings.TrimSpace(h.Get("X-Spam-Status")))
	in.Spam = flag == "yes" || flag == "true" || strings.HasPrefix(status, "yes")
	in.Ours = strings.EqualFold(strings.TrimSpace(h.Get(loopHeader)), domain)

	var plain, html string
	for {
		p, err := r.NextPart()
		if err == io.EOF || p == nil {
			break
		}
		if err != nil {
			// A broken part: what's read so far.
			break
		}
		switch ph := p.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := ph.ContentType()
			body, _ := io.ReadAll(io.LimitReader(p.Body, maxText*4))
			switch {
			case ct == "text/plain" && plain == "":
				plain = string(body)
			case ct == "text/html" && html == "":
				html = string(body)
			case ct != "text/plain" && ct != "text/html":
				if name := partName(ph.Header.Get("Content-Type")); name != "" {
					in.Attachments = append(in.Attachments, name)
				}
			}
		case *mail.AttachmentHeader:
			name, _ := ph.Filename()
			if name == "" {
				name = "?"
			}
			in.Attachments = append(in.Attachments, name)
		}
	}
	switch {
	case strings.TrimSpace(plain) != "":
		in.Text = plain
	case html != "":
		in.Text = HTMLText(html)
	}
	return in, nil
}

func partName(contentType string) string {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	return params["name"]
}

// take turns one fetched mail into netmail. An error leaves it in the
// mailbox for another try; mail that isn't for anyone is dropped.
func (g *Gateway) take(cfg config.EmailConfig, raw []byte, tooBig bool, size uint32) error {
	in, err := Parse(raw, cfg.Domain)
	if err != nil {
		g.drop("unreadable mail: %v", err)
		return nil
	}
	if in.Ours {
		g.drop("mail from %s: sent by this gateway itself", in.From)
		return nil
	}
	if in.Spam && !cfg.DeliverSpam {
		g.drop("mail from %s marked as spam (%q)", in.From, in.Subject)
		return nil
	}
	if in.From == "" {
		g.drop("mail without a sender (%q)", in.Subject)
		return nil
	}
	domain := "@" + strings.ToLower(cfg.Domain)
	delivered := map[int64]bool{}
	for _, to := range in.To {
		if !strings.HasSuffix(to, domain) {
			continue
		}
		local := strings.TrimSuffix(to, domain)
		if i := strings.IndexByte(local, '+'); i > 0 {
			local = local[:i] // swissmaik+bbs@ is swissmaik@
		}
		u, err := g.userByAlias(local)
		if errors.Is(err, user.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if delivered[u.ID] {
			continue
		}
		if !May(cfg, u) {
			g.drop("mail from %s to %s: not allowed to get email", in.From, to)
			delivered[u.ID] = true
			continue
		}
		if err := g.deliver(u, in, tooBig, size); err != nil {
			return err
		}
		delivered[u.ID] = true
	}
	if len(delivered) == 0 {
		g.drop("mail from %s to %s: no such caller", in.From, strings.Join(in.To, ", "))
	}
	return nil
}

func (g *Gateway) drop(format string, args ...any) {
	g.logInfo("email gateway: dropped "+format, args...)
	g.update(func(st *Status) { st.Dropped++ })
}

// deliver stores in as netmail to u.
func (g *Gateway) deliver(u *user.User, in *Incoming, tooBig bool, size uint32) error {
	lang := g.Lang(u)
	text := strings.ReplaceAll(strings.ReplaceAll(in.Text, "\r\n", "\n"), "\r", "\n")
	text = strings.TrimRight(text, "\n ")
	if len(text) > maxText {
		text = text[:maxText] + "\n\n" + i18n.T(lang, "email.cut")
	}
	if tooBig {
		text = i18n.T(lang, "email.too_big", "SIZE", fmt.Sprintf("%.1f MB", float64(size)/(1<<20)))
	}
	if len(in.Attachments) > 0 {
		text += "\n\n" + i18n.T(lang, "email.attachments_dropped", "NAMES", strings.Join(in.Attachments, ", "))
	}
	text = textfmt.WrapLongLines(text, textfmt.LineWidth)
	subject := strings.TrimSpace(in.Subject)
	if subject == "" {
		subject = i18n.T(lang, "email.no_subject")
	}
	_, err := g.Netmail.ReceiveEmail(string(ansi.EncodeCP437(in.FromName)), in.From, u.ID, u.Username,
		string(ansi.EncodeCP437(subject)), string(ansi.EncodeCP437(text)), in.Date, in.MessageID, in.InReplyTo)
	if errors.Is(err, netmail.ErrDuplicate) {
		return nil
	}
	if err != nil {
		return err
	}
	g.update(func(st *Status) { st.Received++ })
	g.logInfo("email from %s to %s: %q", in.From, u.Username, subject)
	return nil
}
