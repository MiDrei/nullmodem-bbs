package qwkdoor

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/db"
	"git.maik.ch/nullmodem/bbs/internal/message"
	"git.maik.ch/nullmodem/bbs/internal/netmail"
	"git.maik.ch/nullmodem/bbs/internal/user"
	"git.maik.ch/nullmodem/kit/qwk"
)

type stores struct {
	messages *message.Store
	netmail  *netmail.Store
	users    *user.Store
}

func newStores(t *testing.T) stores {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return stores{message.NewStore(sqlDB), netmail.NewStore(sqlDB), user.NewStore(sqlDB)}
}

// replies builds a real .REP the way an offline reader does (kit's
// BuildReplyPacket, QWKE kludges included) and parses it back.
func replies(t *testing.T, rs []qwk.Reply) []qwk.PackedMessage {
	t.Helper()
	path := filepath.Join(t.TempDir(), "TEST.REP")
	if err := qwk.BuildReplyPacket(path, "TEST", rs); err != nil {
		t.Fatalf("BuildReplyPacket: %v", err)
	}
	out, err := qwk.ParseReplyPacket(path, "TEST")
	if err != nil {
		t.Fatalf("ParseReplyPacket: %v", err)
	}
	return out
}

func TestRouteRepliesUsesKludgesReachesNameAtAddressAndReportsRejects(t *testing.T) {
	st := newStores(t)
	if _, err := st.users.Register("sysop", "password123", user.SLNewUser); err != nil {
		t.Fatal(err)
	}
	alice, err := st.users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatal(err)
	}
	general, err := st.messages.AreaByTag("general")
	if err != nil {
		t.Fatal(err)
	}
	longSubject := "A subject far longer than the twenty-five bytes a header holds"

	res, err := RouteReplies(st.messages, st.netmail, st.users, "2:301/100", alice, replies(t, []qwk.Reply{
		{Conference: int(general.ID), To: "All", From: "alice", Subject: longSubject, Text: "echo body"},
		{Conference: 0, To: "Hans Muster@2:301/1.5", From: "alice", Subject: "netmail out", Text: "hello Hans"},
		{Conference: 0, To: "nobodyhere", From: "alice", Subject: "lost", Text: "x"},
		{Conference: 9999, To: "All", From: "alice", Subject: "nowhere", Text: "x"},
	}))
	if err != nil {
		t.Fatalf("RouteReplies: %v", err)
	}
	if res.Posted != 1 || res.Sent != 1 || len(res.Rejected) != 2 {
		t.Fatalf("result = %+v, want 1 posted, 1 sent, 2 rejected", res)
	}
	if r := res.Rejected[0]; r.Index != 2 || r.To != "nobodyhere" || !strings.Contains(r.Reason, "unknown recipient") {
		t.Fatalf("first rejection = %+v", r)
	}
	if r := res.Rejected[1]; r.Index != 3 || !strings.Contains(r.Reason, "does not exist") {
		t.Fatalf("second rejection = %+v", r)
	}

	msgs, _ := st.messages.ListMessages(general.ID)
	var posted *message.Message
	for i := range msgs {
		if msgs[i].FromUserID.Valid && msgs[i].FromUserID.Int64 == alice.ID {
			posted = &msgs[i]
		}
	}
	if posted == nil {
		t.Fatal("echo reply not posted")
	}
	if posted.Subject != longSubject {
		t.Fatalf("subject = %q, want the full one from the kludge", posted.Subject)
	}
	if strings.Contains(posted.Body, "Subject:") || strings.TrimSpace(posted.Body) != "echo body" {
		t.Fatalf("body = %q, want the kludge lines stripped", posted.Body)
	}

	sent, err := st.netmail.Sent(alice.ID)
	if err != nil || len(sent) != 1 {
		t.Fatalf("sent netmail = %+v, %v", sent, err)
	}
	if sent[0].ToName != "Hans Muster" || sent[0].ToAddress != "2:301/1.5" {
		t.Fatalf("netmail to %q at %q, want Hans Muster at 2:301/1.5", sent[0].ToName, sent[0].ToAddress)
	}
}

func TestNetmailRecipientForms(t *testing.T) {
	st := newStores(t)
	bob, err := st.users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		to         string
		id         int64
		name, addr string
		ok         bool
	}{
		{"bob", bob.ID, "bob", "", true},
		{"BOB", bob.ID, "bob", "", true},
		{"Hans Muster@2:301/1", 0, "Hans Muster", "2:301/1", true},
		{"2:301/1", 0, "2:301/1", "2:301/1", true},
		{"someone@example.com", 0, "", "", false},
		{"@2:301/1", 0, "", "", false},
		{"", 0, "", "", false},
	} {
		id, name, addr, ok := netmailRecipient(st.users, c.to)
		if id != c.id || name != c.name || addr != c.addr || ok != c.ok {
			t.Errorf("netmailRecipient(%q) = %d %q %q %v, want %d %q %q %v", c.to, id, name, addr, ok, c.id, c.name, c.addr, c.ok)
		}
	}
}

func TestPacketFlagsNetmailAndNamesRemoteSendersWithTheirAddress(t *testing.T) {
	st := newStores(t)
	alice, err := st.users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.netmail.Receive("Hans Muster", "2:301/1.5", alice.ID, "alice", "", "from afar", "hello alice", time.Now(), false); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	res, err := BuildPacketForUser(st.messages, st.netmail, alice, "Test BBS", "Sysop", t.TempDir())
	if err != nil {
		t.Fatalf("BuildPacketForUser: %v", err)
	}
	p, err := qwk.OpenPacket(res.PacketPath)
	if err != nil {
		t.Fatalf("OpenPacket: %v", err)
	}
	defer p.Close()
	if a, ok := p.Ext.Area(0); !ok || !a.IsNetmail() {
		t.Fatalf("conference 0 = %+v, %v; want flagged netmail", a, ok)
	}
	k, _ := qwk.ParseQWKEKludges(p.Messages[0].Text)
	from := k.From
	if from == "" {
		from = p.Messages[0].Header.From
	}
	if from != "Hans Muster@2:301/1.5" {
		t.Fatalf("from = %q, want the sender with their address", from)
	}
}
