package emailgw

import (
	"context"
	"errors"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/midrei/nullmodem-kit/ansi"
)

// fakeDNS answers the mail server's lookups from a table.
type fakeDNS struct {
	txt   map[string][]string
	hosts map[string][]string
}

var errNoSuchHost = &net.DNSError{Err: "no such host", IsNotFound: true}

func (f *fakeDNS) LookupHost(_ context.Context, h string) ([]string, error) {
	if a, ok := f.hosts[h]; ok {
		return a, nil
	}
	return nil, errNoSuchHost
}
func (f *fakeDNS) LookupTXT(_ context.Context, n string) ([]string, error) {
	if a, ok := f.txt[n]; ok {
		return a, nil
	}
	return nil, errNoSuchHost
}
func (f *fakeDNS) LookupMX(context.Context, string) ([]*net.MX, error) { return nil, errNoSuchHost }
func (f *fakeDNS) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return nil, errNoSuchHost
}
func (f *fakeDNS) LookupAddr(context.Context, string) ([]string, error) { return nil, errNoSuchHost }

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// startReceiver runs the gateway's mail server on a free port.
func startReceiver(t *testing.T, e *env, dns *fakeDNS, clk *clock) (*Receiver, string) {
	t.Helper()
	e.cfg.Receive.SMTP = true
	e.cfg.Receive.Listen = "127.0.0.1:0"
	r := &Receiver{Gateway: e.gw, DataDir: t.TempDir(), Resolver: dns, Now: clk.now}
	srv, err := r.start(e.cfg, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	return r, srv.Addr
}

// send delivers one mail over SMTP and returns the server's verdict.
func send(addr, from string, to []string, body string) error {
	c, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Hello("mail.sender.example"); err != nil {
		return err
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, r := range to {
		if err := c.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(strings.ReplaceAll(body, "\n", "\r\n"))); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func code(err error) int {
	var te *textproto.Error
	if errors.As(err, &te) {
		return te.Code
	}
	return 0
}

func TestMailServerTakesMailForTheDomains(t *testing.T) {
	e := setup(t)
	f := false
	e.cfg.Receive.Greylist = &f
	e.cfg.Receive.ExtraDomains = []string{"Other.Example"}
	clk := &clock{t: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
	_, addr := startReceiver(t, e, &fakeDNS{}, clk)

	body := "From: Jurg <jm@sender.example>\nTo: list@sender.example\nSubject: Hello\n\nHi there.\n"
	// The envelope counts: a Bcc to swissmaik at the other domain.
	if err := send(addr, "jm@sender.example", []string{"swissmaik@other.example"}, body); err != nil {
		t.Fatalf("send: %v", err)
	}
	inbox, _ := e.nm.Inbox(e.maik.ID)
	if len(inbox) != 1 || ansi.DecodeCP437([]byte(inbox[0].Subject)) != "Hello" || inbox[0].Email != "jm@sender.example" {
		t.Fatalf("inbox %+v", inbox)
	}
	// Unknown and not-allowed recipients are refused at RCPT.
	for _, to := range []string{"nobody@example.ch", "joe.guest@example.ch", "swissmaik@elsewhere.example"} {
		if err := send(addr, "jm@sender.example", []string{to}, body); code(err) != 550 {
			t.Errorf("%s: %v, want 550", to, err)
		}
	}
	// postmaster@ reaches the sysop.
	if err := send(addr, "jm@sender.example", []string{"postmaster@example.ch"}, body); err != nil {
		t.Fatalf("postmaster: %v", err)
	}
	if inbox, _ := e.nm.Inbox(e.maik.ID); len(inbox) != 2 {
		t.Errorf("postmaster mail not delivered: %d", len(inbox))
	}
}

func TestMailServerGreylistsSPFAndBlockLists(t *testing.T) {
	e := setup(t)
	clk := &clock{t: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
	dns := &fakeDNS{
		txt: map[string][]string{
			"strict.example": {"v=spf1 ip4:192.0.2.1 -all"},
			"soft.example":   {"v=spf1 ip4:192.0.2.1 ~all"},
		},
		hosts: map[string][]string{},
	}
	r, addr := startReceiver(t, e, dns, clk)
	body := "From: a@x.example\nTo: swissmaik@example.ch\nSubject: s\n\nbody\n"

	// Greylisting: the first try is told to wait, a retry too soon too,
	// one after the delay passes -- and later ones straight away.
	if err := send(addr, "a@x.example", []string{"swissmaik@example.ch"}, body); code(err) != 451 {
		t.Fatalf("first try: %v, want 451", err)
	}
	clk.add(time.Minute)
	if err := send(addr, "a@x.example", []string{"swissmaik@example.ch"}, body); code(err) != 451 {
		t.Fatalf("early retry: %v, want 451", err)
	}
	clk.add(5 * time.Minute)
	if err := send(addr, "a@x.example", []string{"swissmaik@example.ch"}, body); err != nil {
		t.Fatalf("retry: %v", err)
	}
	clk.add(48 * time.Hour)
	if err := send(addr, "a@x.example", []string{"swissmaik@example.ch"}, body); err != nil {
		t.Fatalf("known sender: %v", err)
	}

	// SPF: a hard fail is refused at MAIL; a soft fail is spam, dropped
	// while the gateway doesn't deliver spam.
	if err := send(addr, "a@strict.example", []string{"swissmaik@example.ch"}, body); code(err) != 550 {
		t.Errorf("SPF fail: %v, want 550", err)
	}
	f := false
	e.cfg.Receive.Greylist = &f
	before, _ := e.nm.Inbox(e.maik.ID)
	if err := send(addr, "a@soft.example", []string{"swissmaik@example.ch"}, body); err != nil {
		t.Errorf("SPF soft fail: %v", err)
	}
	if after, _ := e.nm.Inbox(e.maik.ID); len(after) != len(before) {
		t.Error("soft-failed mail delivered though spam isn't")
	}

	// Block lists: listed is refused before the greeting; the list
	// refusing the query (127.255.255.x) is no listing.
	dns.hosts["1.0.0.127.zen.spamhaus.org"] = []string{"127.255.255.254"}
	if r.listedAt("127.0.0.1") != "" {
		t.Error("loopback checked against a block list")
	}
	dns.hosts["2.0.0.198.zen.spamhaus.org"] = []string{"127.0.0.4"}
	dns.hosts["3.0.0.198.zen.spamhaus.org"] = []string{"127.255.255.254"}
	if got := r.listedAt("198.0.0.2"); got != "zen.spamhaus.org" {
		t.Errorf("listed address: %q", got)
	}
	if got := r.listedAt("198.0.0.3"); got != "" {
		t.Errorf("refused query taken for a listing: %q", got)
	}
}

func TestMailServerConnectionLimit(t *testing.T) {
	e := setup(t)
	r := &Receiver{Gateway: e.gw, Resolver: &fakeDNS{}}
	for i := 0; i < maxPerIP; i++ {
		if reason := r.admit("192.0.2.9"); reason != "" {
			t.Fatalf("connection %d refused: %s", i+1, reason)
		}
	}
	if r.admit("192.0.2.9") == "" {
		t.Error("connection over the limit admitted")
	}
}
