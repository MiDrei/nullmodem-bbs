package web

import (
	"testing"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

// Each uplink's last good and last failed session, the latter also when
// the dial never got an address back and is kept under the host.
func TestUplinkStatuses(t *testing.T) {
	srv, _, _ := newTestServer(t)
	if _, err := srv.DB.Exec(`INSERT INTO binkp_sessions (direction, peer_address, peer_host, started_at, storage_path, outcome, detail) VALUES
		('outbound', '21:1/100', '', datetime('now', '-2 hours'), 'x', 'ok', ''),
		('outbound', '21:1/100', '', datetime('now', '-1 hours'), 'x', 'error', 'i/o timeout'),
		('outbound', '', 'b.example:24554', datetime('now', '-10 minutes'), 'x', 'error', 'connection refused'),
		('outbound', '2:301/1', '', datetime('now', '-3 days'), 'x', 'ok', '')`); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Binkp.Uplinks = []config.BinkpUplink{
		{Address: "21:1/100", Host: "a.example:24554"},
		{Address: "2:301/1", Host: "b.example:24554"},
	}
	got := srv.uplinkStatuses(cfg)
	if len(got) != 2 {
		t.Fatalf("got %d uplinks", len(got))
	}
	a, b := got[0], got[1]
	if a.LastOK == "" || a.LastError <= a.LastOK || a.Error != "i/o timeout" || a.Sessions24h != 2 || a.Errors24h != 1 {
		t.Errorf("21:1/100: %+v", a)
	}
	if b.LastOK == "" || b.LastError <= b.LastOK || b.Error != "connection refused" || b.Sessions24h != 1 || b.Errors24h != 1 {
		t.Errorf("2:301/1: %+v", b)
	}
}

// The dashboard's email card: none while the gateway is off; on, its
// ways in and the newest mail taken.
func TestEmailBrief(t *testing.T) {
	srv, users, _ := newTestServer(t)
	cfg := config.Default()
	if srv.emailBrief(cfg) != nil {
		t.Error("email brief while the gateway is off")
	}
	u, _ := users.Register("alice", "password123", user.SLNewUser)
	if _, err := srv.Netmail.ReceiveEmail("Joe", "joe@other.example", u.ID, "alice", "Hi", "body", time.Now(), "", ""); err != nil {
		t.Fatal(err)
	}
	cfg.Email = config.EmailConfig{Enabled: true, Domain: "example.ch", IMAP: config.MailServer{Host: "imap.example.ch"},
		Receive: config.MailReceive{Webhook: true}}
	b := srv.emailBrief(cfg)
	if b == nil || !b.Mailbox || !b.Webhook || b.Server != nil || b.LastIn == "" {
		t.Fatalf("brief %+v", b)
	}
}
