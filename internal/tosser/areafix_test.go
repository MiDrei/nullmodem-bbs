package tosser

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/areafix"
	"github.com/midrei/nullmodem-bbs/internal/binkp"
	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/db"
	"github.com/midrei/nullmodem-bbs/internal/mail"
	"github.com/midrei/nullmodem-bbs/internal/message"
	"github.com/midrei/nullmodem-bbs/internal/netmail"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

func newTestStoresWithAreafix(t *testing.T) (*netmail.Store, *message.Store, *user.Store, *areafix.EchoStore, *areafix.FileStore) {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return netmail.NewStore(sqlDB), message.NewStore(sqlDB), user.NewStore(sqlDB),
		areafix.NewEchoStore(sqlDB), areafix.NewFileStore(sqlDB)
}

func TestRequestEchoAreaSubscriptionComposesAndRecordsRequest(t *testing.T) {
	netmailStore, _, _, subs, _ := newTestStoresWithAreafix(t)
	uplink := config.BinkpUplink{
		Address:         "21:3/100",
		Host:            "hub.example.com:24554",
		AreafixPassword: "secret1",
	}

	msg, err := RequestEchoAreaSubscription(netmailStore, subs, []string{"21:3/194.1"}, "Test BBS", uplink, "FSX_GEN", true)
	if err != nil {
		t.Fatalf("RequestEchoAreaSubscription: %v", err)
	}

	if msg.ToName != "Areafix" || msg.ToAddress != "21:3/100" {
		t.Fatalf("message addressed to %q at %q, want Areafix at 21:3/100", msg.ToName, msg.ToAddress)
	}
	// FromName must be our own identity (bbsName), never "Areafix"
	// again -- handleAreafixRequest's reply mirrors this request's own
	// FromName back as its reply's ToName, so if this were "Areafix"
	// too, our own inbound robot would misidentify that reply as a
	// fresh incoming request instead of a reply to one we sent (see
	// requestAreaCommand's doc comment).
	if msg.FromName != "Test BBS" {
		t.Fatalf("message FromName = %q, want our own bbsName (Test BBS), not the remote robot's name", msg.FromName)
	}
	if !msg.Crash {
		t.Fatal("message.Crash = false, want true (dialed immediately, not on next scheduled poll)")
	}
	// The password goes in the Subject, not the body -- confirmed live
	// against a real Areafix robot (Clearing Houz), which authenticates
	// the Subject specifically.
	if msg.Subject != "secret1" {
		t.Fatalf("message Subject = %q, want the password %q", msg.Subject, "secret1")
	}
	wantBody := "+FSX_GEN\r"
	if msg.Body != wantBody {
		t.Fatalf("message body = %q, want %q", msg.Body, wantBody)
	}

	list, err := subs.ListForUplink(uplink.Host, areafix.Outbound)
	if err != nil {
		t.Fatalf("ListForUplink: %v", err)
	}
	if len(list) != 1 || list[0].AreaTag != "FSX_GEN" {
		t.Fatalf("got %+v, want exactly one FSX_GEN subscription recorded", list)
	}
}

func TestRequestEchoAreaSubscriptionUnsubscribeWithdrawsRecord(t *testing.T) {
	netmailStore, _, _, subs, _ := newTestStoresWithAreafix(t)
	uplink := config.BinkpUplink{Address: "21:3/100", Host: "hub.example.com:24554", AreafixPassword: "secret1"}

	if _, err := RequestEchoAreaSubscription(netmailStore, subs, []string{"21:3/194.1"}, "Test BBS", uplink, "FSX_GEN", true); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	msg, err := RequestEchoAreaSubscription(netmailStore, subs, []string{"21:3/194.1"}, "Test BBS", uplink, "FSX_GEN", false)
	if err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	if msg.Subject != "secret1" {
		t.Fatalf("unsubscribe Subject = %q, want the password %q", msg.Subject, "secret1")
	}
	wantBody := "-FSX_GEN\r"
	if msg.Body != wantBody {
		t.Fatalf("unsubscribe body = %q, want %q", msg.Body, wantBody)
	}

	list, err := subs.ListForUplink(uplink.Host, areafix.Outbound)
	if err != nil {
		t.Fatalf("ListForUplink: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("got %d subscriptions after unsubscribe, want 0", len(list))
	}
}

// TestRequestFileAreaSubscriptionUsesFilefixRobotAndPassword confirms
// the file-echo variant addresses Filefix (not Areafix) and uses the
// distinct FilefixPassword field.
func TestRequestFileAreaSubscriptionUsesFilefixRobotAndPassword(t *testing.T) {
	netmailStore, _, _, _, fileSubs := newTestStoresWithAreafix(t)
	uplink := config.BinkpUplink{
		Address:         "21:3/100",
		Host:            "hub.example.com:24554",
		AreafixPassword: "wrong-one",
		FilefixPassword: "filesecret",
	}

	msg, err := RequestFileAreaSubscription(netmailStore, fileSubs, []string{"21:3/194.1"}, "Test BBS", uplink, "FSX_FILES", true)
	if err != nil {
		t.Fatalf("RequestFileAreaSubscription: %v", err)
	}
	if msg.ToName != "Filefix" {
		t.Fatalf("ToName = %q, want Filefix", msg.ToName)
	}
	if msg.Subject != "filesecret" {
		t.Fatalf("message Subject = %q, want the password %q", msg.Subject, "filesecret")
	}
	wantBody := "+FSX_FILES\r"
	if msg.Body != wantBody {
		t.Fatalf("message body = %q, want %q", msg.Body, wantBody)
	}
}

// TestRequestEchoAreaSubscriptionRoutesToExactUplinkOnly is an
// end-to-end test through Poll/RoutedOutbound: with two configured
// uplinks sharing different zones, a Crash-priority Areafix request
// addressed to one of them must be sent ONLY to that uplink, never
// picked up by the other, even though both are otherwise eligible
// "non-disabled" uplinks for ordinary mail.
func TestRequestEchoAreaSubscriptionRoutesToExactUplinkOnly(t *testing.T) {
	netmailStore, messages, users, subs, _ := newTestStoresWithAreafix(t)

	fsxUplink := config.BinkpUplink{Address: "21:3/100", Host: "unused:1", AreafixPassword: "pw1"}
	hobbyUplink := config.BinkpUplink{Address: "954:700/1", Host: "unused:2", AreafixPassword: "pw2"}
	allUplinks := []config.BinkpUplink{fsxUplink, hobbyUplink}

	if _, err := RequestEchoAreaSubscription(netmailStore, subs, []string{"21:3/194.1", "954:700/14"}, "Test BBS", fsxUplink, "FSX_GEN", true); err != nil {
		t.Fatalf("RequestEchoAreaSubscription: %v", err)
	}

	routedToFsx, err := RoutedOutbound(netmailStore, fsxUplink, allUplinks)
	if err != nil {
		t.Fatalf("RoutedOutbound(fsx): %v", err)
	}
	routedToHobby, err := RoutedOutbound(netmailStore, hobbyUplink, allUplinks)
	if err != nil {
		t.Fatalf("RoutedOutbound(hobby): %v", err)
	}
	if len(routedToFsx) != 1 {
		t.Fatalf("routed to fsx uplink = %d messages, want 1", len(routedToFsx))
	}
	if len(routedToHobby) != 0 {
		t.Fatalf("routed to hobby uplink = %d messages, want 0 (must not also go to the wrong uplink)", len(routedToHobby))
	}

	_ = users
	_ = messages
}

// TestRequestEchoAreaChangesBatchesMultipleAreasIntoOneMessage covers
// the "select several areas, one request" case the web UI drives:
// several AreaChanges must land in a single netmail with one +/-TAG
// line each, and every one of them recorded.
func TestRequestEchoAreaChangesBatchesMultipleAreasIntoOneMessage(t *testing.T) {
	netmailStore, _, _, subs, _ := newTestStoresWithAreafix(t)
	uplink := config.BinkpUplink{Address: "21:3/100", Host: "hub.example.com:24554", AreafixPassword: "pw1"}

	changes := []AreaChange{
		{Tag: "FSX_GEN", Subscribe: true},
		{Tag: "FSX_ADS", Subscribe: true},
		{Tag: "FSX_OLD", Subscribe: false},
	}
	msg, err := RequestEchoAreaChanges(netmailStore, subs, []string{"21:3/194.1"}, "Test BBS", uplink, changes)
	if err != nil {
		t.Fatalf("RequestEchoAreaChanges: %v", err)
	}
	if msg.Subject != "pw1" {
		t.Fatalf("message Subject = %q, want the password %q", msg.Subject, "pw1")
	}
	wantBody := "+FSX_GEN\r+FSX_ADS\r-FSX_OLD\r"
	if msg.Body != wantBody {
		t.Fatalf("message body = %q, want %q", msg.Body, wantBody)
	}

	subbed, err := subs.ListForUplink(uplink.Host, areafix.Outbound)
	if err != nil {
		t.Fatalf("ListForUplink: %v", err)
	}
	if len(subbed) != 2 {
		t.Fatalf("got %d recorded subscriptions, want 2 (FSX_OLD withdrawn, not recorded)", len(subbed))
	}
}

// TestRequestEchoAreaListSendsPercentListCommand covers the "request
// the uplink's area catalog" case.
func TestRequestEchoAreaListSendsPercentListCommand(t *testing.T) {
	netmailStore, _, _, _, _ := newTestStoresWithAreafix(t)
	uplink := config.BinkpUplink{Address: "21:3/100", Host: "hub.example.com:24554", AreafixPassword: "pw1"}

	msg, err := RequestEchoAreaList(netmailStore, []string{"21:3/194.1"}, "Test BBS", uplink)
	if err != nil {
		t.Fatalf("RequestEchoAreaList: %v", err)
	}
	if msg.Subject != "pw1" {
		t.Fatalf("message Subject = %q, want the password %q", msg.Subject, "pw1")
	}
	wantBody := "%LIST\r"
	if msg.Body != wantBody {
		t.Fatalf("message body = %q, want %q", msg.Body, wantBody)
	}
	if msg.ToName != "Areafix" || !msg.Crash {
		t.Fatalf("message ToName/Crash = %q/%v, want Areafix/true", msg.ToName, msg.Crash)
	}
}

// TestRequestEchoAreaListDoesNotGetATearlineWhenSent is a regression
// test for a real production bug: buildPacket unconditionally
// appended a tearline/origin line to every outbound netmail,
// including a system-composed Areafix/Filefix request with no local
// user origin. Confirmed live against a real Areafix robot (Clearing
// Houz): it's addressed to an automated line-by-line command parser,
// not a person, and inserting extra content risked confusing it --
// the wire body must be exactly the queued one, untouched.
func TestRequestEchoAreaListDoesNotGetATearlineWhenSent(t *testing.T) {
	netmailStore, messages, users, _, _ := newTestStoresWithAreafix(t)

	var mu sync.Mutex
	var received []byte
	addr, done := runFakeUplink(t, binkp.Config{
		OurAddresses: []string{"21:3/100"},
		ReceiveFile: func(f binkp.InboundFile, r io.Reader) error {
			data, err := io.ReadAll(r)
			mu.Lock()
			received = data
			mu.Unlock()
			return err
		},
	})
	uplink := config.BinkpUplink{Address: "21:3/100", Host: addr, AreafixPassword: "pw1"}

	// A non-point address, matching a real leaf system's own AKA --
	// no FMPT/INTL kludge to account for, just the request itself.
	if _, err := RequestEchoAreaList(netmailStore, []string{"21:3/194"}, "Test BBS", uplink); err != nil {
		t.Fatalf("RequestEchoAreaList: %v", err)
	}

	if _, err := Poll(context.Background(), []string{"21:3/194"}, "Test BBS", uplink, nil, netmailStore, messages, users, nil, nil, nil); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if out := <-done; out.err != nil {
		t.Fatalf("fake uplink answerer error: %v", out.err)
	}

	mu.Lock()
	pkt := received
	mu.Unlock()
	p, err := mail.ReadPacket(bytes.NewReader(pkt))
	if err != nil {
		t.Fatalf("parsing sent packet: %v", err)
	}
	if len(p.Messages) != 1 {
		t.Fatalf("sent packet has %d messages, want 1", len(p.Messages))
	}
	if p.Messages[0].Subject != "pw1" {
		t.Fatalf("sent message Subject = %q, want the password %q", p.Messages[0].Subject, "pw1")
	}
	wantBody := "%LIST\n"
	if p.Messages[0].Body != wantBody {
		t.Fatalf("sent message body = %q, want exactly %q (no tearline/origin appended)", p.Messages[0].Body, wantBody)
	}
}

// TestRequestEchoAreaSubscriptionActuallySendsViaPoll exercises the
// full send path: Poll picks up the queued request and delivers it to
// a fake uplink over a real BinkP session.
func TestRequestEchoAreaSubscriptionActuallySendsViaPoll(t *testing.T) {
	netmailStore, messages, users, subs, _ := newTestStoresWithAreafix(t)

	var mu sync.Mutex
	var received []byte
	addr, done := runFakeUplink(t, binkp.Config{
		OurAddresses: []string{"21:3/100"},
		ReceiveFile: func(f binkp.InboundFile, r io.Reader) error {
			data, err := io.ReadAll(r)
			mu.Lock()
			received = data
			mu.Unlock()
			return err
		},
	})
	uplink := config.BinkpUplink{Address: "21:3/100", Host: addr, AreafixPassword: "pw1"}

	if _, err := RequestEchoAreaSubscription(netmailStore, subs, []string{"21:3/194.1"}, "Test BBS", uplink, "FSX_GEN", true); err != nil {
		t.Fatalf("RequestEchoAreaSubscription: %v", err)
	}

	res, err := Poll(context.Background(), []string{"21:3/194.1"}, "Test BBS", uplink, nil, netmailStore, messages, users, nil, nil, nil)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if out := <-done; out.err != nil {
		t.Fatalf("fake uplink answerer error: %v", out.err)
	}
	if res.Sent != 1 {
		t.Fatalf("Result.Sent = %d, want 1", res.Sent)
	}

	mu.Lock()
	pkt := received
	mu.Unlock()
	p, err := mail.ReadPacket(bytes.NewReader(pkt))
	if err != nil {
		t.Fatalf("parsing sent packet: %v", err)
	}
	if len(p.Messages) != 1 {
		t.Fatalf("sent packet has %d messages, want 1", len(p.Messages))
	}
	if p.Messages[0].ToName != "Areafix" {
		t.Fatalf("sent message ToName = %q, want Areafix", p.Messages[0].ToName)
	}
	if !strings.Contains(p.Messages[0].Body, "+FSX_GEN") {
		t.Fatalf("sent message body = %q, want it to contain %q", p.Messages[0].Body, "+FSX_GEN")
	}
}
