package emailgw

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend/memory"
	imapclient "github.com/emersion/go-imap/client"
	imapserver "github.com/emersion/go-imap/server"
	"github.com/emersion/go-smtp"

	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/db"
	"git.maik.ch/nullmodem/bbs/internal/netmail"
	"git.maik.ch/nullmodem/bbs/internal/user"
	"git.maik.ch/nullmodem/kit/ansi"
)

func TestAlias(t *testing.T) {
	for in, want := range map[string]string{
		"SwissMaik":   "swissmaik",
		"Joe Bloggs":  "joe.bloggs",
		"  A..B  ":    "a.b",
		"Zoë_x-1":     "zo_x-1",
		"!!!":         "",
		"Dr. Who":     "dr.who",
		"trailing. ":  "trailing",
		"Mr  Spaces ": "mr.spaces",
	} {
		if got := Alias(in); got != want {
			t.Errorf("Alias(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsEmailAddress(t *testing.T) {
	for s, want := range map[string]bool{
		"joe@example.com":         true,
		"joe.b+x@mail.example.ch": true,
		"21:3/194":                false,
		"Joe@21:3/194":            false,
		"joe@localhost":           false,
		"joe":                     false,
		"Joe Bloggs <joe@x.ch>":   false,
		"@example.com":            false,
	} {
		if got := netmail.IsEmailAddress(s); got != want {
			t.Errorf("IsEmailAddress(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestHTMLText(t *testing.T) {
	got := HTMLText(`<html><head><style>p{}</style></head><body><p>Hello <b>there</b>,</p>
		<p>see <a href="https://example.com/x">this</a>.<br>Bye</p><ul><li>one</li><li>two &amp; three</li></ul><script>x()</script></body></html>`)
	want := "Hello there,\n\nsee this <https://example.com/x>.\nBye\n\n- one\n- two & three"
	if got != want {
		t.Errorf("HTMLText:\n%q\nwant\n%q", got, want)
	}
}

// fakeSMTP collects what's sent to it.
type fakeSMTP struct {
	mu   sync.Mutex
	got  []sentMail
	fail int // answer RCPT with this code (0: accept)
}

type sentMail struct {
	from string
	to   []string
	data string
}

type smtpSession struct {
	f   *fakeSMTP
	cur sentMail
}

func (s *smtpSession) Reset()        { s.cur = sentMail{} }
func (s *smtpSession) Logout() error { return nil }
func (s *smtpSession) Mail(from string, _ *smtp.MailOptions) error {
	s.cur.from = from
	return nil
}
func (s *smtpSession) Rcpt(to string, _ *smtp.RcptOptions) error {
	if s.f.fail != 0 {
		return &smtp.SMTPError{Code: s.f.fail, Message: "no such mailbox"}
	}
	s.cur.to = append(s.cur.to, to)
	return nil
}
func (s *smtpSession) Data(r io.Reader) error {
	b, _ := io.ReadAll(r)
	s.cur.data = string(b)
	s.f.mu.Lock()
	s.f.got = append(s.f.got, s.cur)
	s.f.mu.Unlock()
	return nil
}

func startSMTP(t *testing.T, f *fakeSMTP) int {
	t.Helper()
	srv := smtp.NewServer(smtp.BackendFunc(func(*smtp.Conn) (smtp.Session, error) { return &smtpSession{f: f}, nil }))
	srv.Domain = "localhost"
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	return l.Addr().(*net.TCPAddr).Port
}

func startIMAP(t *testing.T) int {
	t.Helper()
	srv := imapserver.New(memory.New())
	srv.AllowInsecureAuth = true
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	return l.Addr().(*net.TCPAddr).Port
}

// deliverIMAP puts a mail into the test mailbox.
func deliverIMAP(t *testing.T, port int, raw string) {
	t.Helper()
	c, err := imapclient.Dial("127.0.0.1:" + strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Logout()
	if err := c.Login("username", "password"); err != nil {
		t.Fatal(err)
	}
	if err := c.Append("INBOX", nil, time.Now(), bytes.NewBufferString(strings.ReplaceAll(raw, "\n", "\r\n"))); err != nil {
		t.Fatal(err)
	}
}

func unseen(t *testing.T, port int) int {
	t.Helper()
	c, err := imapclient.Dial("127.0.0.1:" + strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Logout()
	c.Login("username", "password")
	c.Select("INBOX", true)
	crit := imap.NewSearchCriteria()
	crit.WithoutFlags = []string{imap.SeenFlag}
	ids, err := c.Search(crit)
	if err != nil {
		t.Fatal(err)
	}
	return len(ids)
}

type env struct {
	gw    *Gateway
	cfg   config.EmailConfig
	nm    *netmail.Store
	users *user.Store
	db    *sql.DB
	smtp  *fakeSMTP
	imap  int
	maik  *user.User
	guest *user.User
}

func setup(t *testing.T) *env {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	users := user.NewStore(sqlDB)
	maik, err := users.Register("SwissMaik", "secret-pass", 255)
	if err != nil {
		t.Fatal(err)
	}
	guest, err := users.RegisterNew("Joe Guest", "secret-pass", 5, true)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{nm: netmail.NewStore(sqlDB), users: users, db: sqlDB, smtp: &fakeSMTP{}, maik: maik, guest: guest}
	e.imap = startIMAP(t)
	smtpPort := startSMTP(t, e.smtp)
	e.cfg = config.EmailConfig{
		Enabled: true, Domain: "Example.CH", MinSL: 20,
		IMAP: config.MailServer{Host: "127.0.0.1", Port: e.imap, Security: "none", User: "username", Password: "password"},
		SMTP: config.MailServer{Host: "127.0.0.1", Port: smtpPort, Security: "none"},
	}
	e.gw = &Gateway{DB: sqlDB, Netmail: e.nm, Users: users,
		Config: func() config.EmailConfig { return e.cfg },
		Lang:   func(*user.User) string { return "en" }, BBSName: func() string { return "Test BBS" }}
	return e
}

func TestFetchDeliversToTheAlias(t *testing.T) {
	e := setup(t)
	deliverIMAP(t, e.imap, `From: =?utf-8?q?J=C3=BCrg_M=C3=BCller?= <jm@other.ch>
To: SwissMaik+bbs@example.ch
Subject: =?utf-8?q?Gr=C3=BCezi?=
Date: Sat, 03 Oct 2026 10:00:00 +0200
Message-ID: <abc@other.ch>
Content-Type: multipart/mixed; boundary=XX

--XX
Content-Type: text/plain; charset=iso-8859-1
Content-Transfer-Encoding: quoted-printable

Sch=F6n, dass es klappt.
--XX
Content-Type: application/pdf
Content-Disposition: attachment; filename=plan.pdf

AAAA
--XX--
`)
	// Not for anyone here, spam, and one for an account not approved yet.
	deliverIMAP(t, e.imap, "From: x@y.ch\nTo: nobody@example.ch\nSubject: hi\n\nhi\n")
	deliverIMAP(t, e.imap, "From: x@y.ch\nTo: swissmaik@example.ch\nX-Spam-Flag: YES\nSubject: buy\n\nbuy\n")
	deliverIMAP(t, e.imap, "From: x@y.ch\nTo: joe.guest@example.ch\nSubject: hi joe\n\nhi\n")

	e.gw.Fetch(context.Background(), e.cfg)
	if st := LoadStatus(e.db); st.LastFetchError != "" || st.Received != 1 || st.Dropped != 3 {
		// 3: nobody, spam, Joe (the backend's sample mail is read already).
		t.Fatalf("status %+v", st)
	}
	inbox, err := e.nm.Inbox(e.maik.ID)
	if err != nil || len(inbox) != 1 {
		t.Fatalf("inbox %v %v", inbox, err)
	}
	m := inbox[0]
	if m.Email != "jm@other.ch" || ansi.DecodeCP437([]byte(m.FromName)) != "Jürg Müller" || ansi.DecodeCP437([]byte(m.Subject)) != "Grüezi" {
		t.Errorf("message %+v", m)
	}
	body := ansi.DecodeCP437([]byte(m.Body))
	if !strings.Contains(body, "Schön, dass es klappt.") || !strings.Contains(body, "plan.pdf") {
		t.Errorf("body %q", body)
	}
	if got, _ := e.nm.Inbox(e.guest.ID); len(got) != 0 {
		t.Errorf("Joe (not approved) got %d mail", len(got))
	}
	if n := unseen(t, e.imap); n != 0 {
		t.Errorf("%d mail still unread in the mailbox", n)
	}
	// A second fetch takes nothing again.
	e.gw.Fetch(context.Background(), e.cfg)
	if inbox, _ := e.nm.Inbox(e.maik.ID); len(inbox) != 1 {
		t.Errorf("inbox after a second fetch: %d", len(inbox))
	}

	// The reply goes out as a reply.
	reply, err := e.nm.SendEmail(e.maik.ID, "", m.Email, "Re: Gr\x81ezi", "Gern geschehen.\n\x01MSGID: x", m.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.gw.SendPending(context.Background(), e.cfg, time.Now())
	if len(e.smtp.got) != 1 {
		t.Fatalf("sent %d", len(e.smtp.got))
	}
	s := e.smtp.got[0]
	if s.from != "swissmaik@example.ch" || len(s.to) != 1 || s.to[0] != "jm@other.ch" {
		t.Errorf("envelope %+v", s)
	}
	for _, want := range []string{"From: SwissMaik <swissmaik@example.ch>", "In-Reply-To: <abc@other.ch>",
		"Subject: =?utf-8?q?Re:_Gr=C3=BCezi?=", "Gern geschehen.", "X-NullModem-Gateway: example.ch",
		"This message comes from SwissMaik, a caller of Test BBS."} {
		if !strings.Contains(s.data, want) {
			t.Errorf("mail lacks %q:\n%s", want, s.data)
		}
	}
	if strings.Contains(s.data, "MSGID") {
		t.Errorf("kludge in the mail:\n%s", s.data)
	}
	if got, _ := e.nm.MessageByID(reply.ID); !got.IsSent() {
		t.Error("reply not marked sent")
	}

	// Our own mail coming back (a loop) is dropped.
	deliverIMAP(t, e.imap, s.data)
	e.gw.Fetch(context.Background(), e.cfg)
	if inbox, _ := e.nm.Inbox(e.maik.ID); len(inbox) != 1 {
		t.Errorf("own mail taken back in: %d", len(inbox))
	}
}

func TestRefusedMailComesBack(t *testing.T) {
	e := setup(t)
	e.smtp.fail = 550
	if _, err := e.nm.SendEmail(e.maik.ID, "", "gone@other.ch", "Hello", "Hi", 0); err != nil {
		t.Fatal(err)
	}
	e.gw.SendPending(context.Background(), e.cfg, time.Now())
	inbox, _ := e.nm.Inbox(e.maik.ID)
	if len(inbox) != 1 || !strings.Contains(inbox[0].Body, "gone@other.ch") || !strings.Contains(inbox[0].Body, "550") {
		t.Fatalf("no bounce: %+v", inbox)
	}
	if due, _ := e.nm.PendingEmail(time.Now().Add(time.Hour)); len(due) != 0 {
		t.Errorf("still pending after a refusal: %d", len(due))
	}
}

func TestUnreachableServerRetries(t *testing.T) {
	e := setup(t)
	e.cfg.SMTP.Port = 1 // nothing listens there
	if _, err := e.nm.SendEmail(e.maik.ID, "", "x@other.ch", "Hello", "Hi", 0); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	e.gw.SendPending(context.Background(), e.cfg, now)
	if due, _ := e.nm.PendingEmail(now); len(due) != 0 {
		t.Error("tried again at once")
	}
	if due, _ := e.nm.PendingEmail(now.Add(5 * time.Minute)); len(due) != 1 {
		t.Error("not tried again later")
	}
	if inbox, _ := e.nm.Inbox(e.maik.ID); len(inbox) != 0 {
		t.Error("bounced on a passing error")
	}
}

func TestCheckSend(t *testing.T) {
	e := setup(t)
	one := 1
	e.cfg.DailyLimit = &one
	if err := CheckSend(e.cfg, e.nm, e.guest, time.Now()); err != ErrNotAllowed {
		t.Errorf("guest: %v", err)
	}
	if err := CheckSend(e.cfg, e.nm, e.maik, time.Now()); err != nil {
		t.Errorf("maik: %v", err)
	}
	e.nm.SendEmail(e.maik.ID, "", "x@other.ch", "Hello", "Hi", 0)
	if err := CheckSend(e.cfg, e.nm, e.maik, time.Now()); err != ErrLimit {
		t.Errorf("maik over the limit: %v", err)
	}
	e.cfg.Enabled = false
	if err := CheckSend(e.cfg, e.nm, e.maik, time.Now()); err != ErrOff {
		t.Errorf("off: %v", err)
	}
}
