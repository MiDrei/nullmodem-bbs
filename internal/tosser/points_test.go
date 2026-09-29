package tosser

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/areafix"
	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/mail"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

// The sysop's reader app as a point of 21:3/194, in two networks.
var (
	pointOurAddresses = []string{"21:3/194@fsxnet", "954:700/14@hobbynet"}
	fsxHub            = config.BinkpUplink{Address: "21:3/100", Host: "hub.fsx:24554", Network: "fsxNet"}
	readerFsx         = config.BinkpUplink{Address: "21:3/194.1", Host: "fidomail", Network: "fsxNet", Downlink: true, Hold: true, PostAs: "sysop"}
	readerHobby       = config.BinkpUplink{Address: "954:700/14.1", Host: "fidomail", Network: "HobbyNet", Downlink: true, Hold: true, PostAs: "sysop"}
	pointUplinks      = []config.BinkpUplink{fsxHub, readerFsx, readerHobby}
)

// pointPacket is a packet from the reader app: one message, written by
// it as the point, with its own kludges, tearline and origin.
func pointPacket(t *testing.T, m mail.Message) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	pw, err := mail.NewWriter(&buf, mail.PacketHeader{OrigAddr: mustAddr(t, "21:3/194.1"), DestAddr: mustAddr(t, "21:3/194")})
	if err != nil {
		t.Fatal(err)
	}
	if err := pw.WriteMessage(m); err != nil {
		t.Fatal(err)
	}
	if err := pw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf
}

func mustAddr(t *testing.T, s string) mail.Address {
	t.Helper()
	a, err := mail.ParseAddress(s)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestPointEchomailIsPostedAsItsUserAndGoesUpNotBack(t *testing.T) {
	netmailStore, messages, _, users, echoSubs, _ := newTestStoresWithRobot(t)
	sysop, _ := users.Register("sysop", "password123", user.SLSysop)
	area, _ := messages.CreateArea("FSX_GEN", "fsxNet General", "", "fsxNet", 0, 0)
	echoSubs.Request("fidomail", "FSX_GEN", areafix.Inbound)

	poster, err := newPointPoster(readerFsx, pointUplinks, users, pointOurAddresses)
	if err != nil || poster == nil || poster.user.ID != sysop.ID {
		t.Fatalf("newPointPoster = %+v, %v", poster, err)
	}
	body := "AREA:FSX_GEN\n\x01MSGID: 21:3/194.1 0000abcd\n\x01PID: FidoMail 1.0\nHello from the iPad.\nSecond line.\n\n--- FidoMail 1.0\n * Origin: My iPad (21:3/194.1)\nSEEN-BY: 3/194\n\x01PATH: 3/194\n"
	msg := mail.Message{OrigAddr: mustAddr(t, "21:3/194.1"), DestAddr: mustAddr(t, "21:3/194"), Written: time.Now(), FromName: "Mike", ToName: "All", Subject: "Test", Body: body}
	for i := 0; i < 2; i++ { // the second time: the reader resent the packet
		stats, err := tossInbound(pointPacket(t, msg), nil, netmailStore, messages, users, nil, poster)
		if err != nil {
			t.Fatalf("tossInbound: %v", err)
		}
		if want := 1 - i; stats.echo != want {
			t.Fatalf("round %d: stats.echo = %d, want %d", i, stats.echo, want)
		}
	}

	stored, _ := messages.ListMessages(area.ID)
	if len(stored) != 1 || !stored[0].FromUserID.Valid || stored[0].FromUserID.Int64 != sysop.ID {
		t.Fatalf("stored = %+v, want one post by sysop", stored)
	}
	if got := stored[0].Body; got != "Hello from the iPad.\nSecond line." {
		t.Fatalf("stored body = %q, want the text without kludges, tearline and origin", got)
	}

	up, _ := RoutedOutboundEcho(messages, fsxHub)
	if len(up) != 1 {
		t.Fatalf("RoutedOutboundEcho(hub) = %d messages, want the point's post going up", len(up))
	}
	buf, err := buildPacket(mustAddr(t, "21:3/194"), mustAddr(t, "21:3/100"), "", "Maiks Place BBS", nil, up, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := strings.ReplaceAll(buf.String(), "\r", "\n")
	if !strings.Contains(out, "MSGID: 21:3/194 ") || !strings.Contains(out, "* Origin: Maiks Place BBS (21:3/194)") || strings.Contains(out, "21:3/194.1") || strings.Contains(out, "My iPad") {
		t.Fatalf("outgoing packet should carry only the BBS's address and origin:\n%s", out)
	}

	back, _ := RoutedOutboundEchoForward(messages, echoSubs, readerFsx)
	if len(back) != 0 {
		t.Fatalf("the point's own post was offered back to it: %+v", back)
	}
	if mine, _ := RoutedOutboundEcho(messages, readerFsx); len(mine) != 0 {
		t.Fatalf("RoutedOutboundEcho(point) = %+v, want nothing: this system's posts go up, not to a point", mine)
	}
}

func TestPointGetsSubscribedAreasOnceAndNotOldBacklog(t *testing.T) {
	_, messages, _, _, echoSubs, _ := newTestStoresWithRobot(t)
	area, _ := messages.CreateArea("FSX_GEN", "fsxNet General", "", "fsxNet", 0, 0)
	// Arrived from the hub: SEEN-BY naming our own 3/194, as always.
	messages.ReceiveEcho(area.ID, "Someone", "New", "text\nSEEN-BY: 3/100 3/194", "21:3/100 00000001", time.Now())
	messages.ReceiveEcho(area.ID, "Someone", "Old", "text", "21:3/100 00000002", time.Now().Add(-60*24*time.Hour))
	echoSubs.Request("fidomail", "FSX_GEN", areafix.Inbound)

	out, err := RoutedOutboundEchoForward(messages, echoSubs, readerFsx)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Subject != "New" {
		t.Fatalf("forward = %+v, want only the recent message (SEEN-BY 3/194 is us, not the point)", out)
	}
	b := &outboundBundle{packetName: "x.pkt", forwardedEcho: out, point: true, pointHost: "fidomail"}
	if err := b.markSent(&Result{}, []string{"x.pkt"}, mustAddr(t, "21:3/194.1"), nil, messages, nil); err != nil {
		t.Fatal(err)
	}
	if again, _ := RoutedOutboundEchoForward(messages, echoSubs, readerFsx); len(again) != 0 {
		t.Fatalf("delivered message offered again: %+v", again)
	}
}

func TestPointNetmailIsSentAsItsUserAndRoutedAway(t *testing.T) {
	netmailStore, messages, _, users, _, _ := newTestStoresWithRobot(t)
	sysop, _ := users.Register("sysop", "password123", user.SLSysop)
	bob, _ := users.Register("bob", "password123", user.SLNewUser)
	poster, _ := newPointPoster(readerFsx, pointUplinks, users, pointOurAddresses)

	remote := mail.Message{OrigAddr: mustAddr(t, "21:3/194.1"), DestAddr: mustAddr(t, "21:1/100"), Written: time.Now(),
		FromName: "Mike", ToName: "Avon", Subject: "Hi", Body: "\x01MSGID: 21:3/194.1 1\nHello Avon\n--- FidoMail\n * Origin: iPad (21:3/194.1)\n"}
	local := mail.Message{OrigAddr: mustAddr(t, "21:3/194.1"), DestAddr: mustAddr(t, "21:3/194"), Written: time.Now(),
		FromName: "Mike", ToName: "bob", Subject: "Local", Body: "Hi Bob\n"}
	for _, m := range []mail.Message{remote, local} {
		if _, err := tossInbound(pointPacket(t, m), nil, netmailStore, messages, users, nil, poster); err != nil {
			t.Fatalf("tossInbound: %v", err)
		}
	}

	pending, _ := netmailStore.PendingOutbound()
	if len(pending) != 1 || pending[0].FromUserID.Int64 != sysop.ID || pending[0].FromAddress != "21:3/194" || pending[0].ToAddress != "21:1/100" || pending[0].Body != "Hello Avon" {
		t.Fatalf("pending = %+v, want the netmail to Avon from sysop@21:3/194", pending)
	}
	if got := routeOutbound(pending, fsxHub, pointUplinks); len(got) != 1 {
		t.Fatalf("hub gets %d, want the point's netmail", len(got))
	}
	if got := routeOutbound(pending, readerFsx, pointUplinks); len(got) != 0 {
		t.Fatalf("point gets %+v, want nothing but mail addressed to it", got)
	}
	inbox, _ := netmailStore.Inbox(bob.ID)
	if len(inbox) != 1 || inbox[0].FromUserID.Int64 != sysop.ID {
		t.Fatalf("bob's inbox = %+v, want the local netmail from sysop", inbox)
	}
}

func TestNetmailToPostAsUserIsCopiedToThePointOnce(t *testing.T) {
	netmailStore, _, _, users, _, _ := newTestStoresWithRobot(t)
	sysop, _ := users.Register("sysop", "password123", user.SLSysop)
	poster, _ := newPointPoster(readerHobby, pointUplinks, users, pointOurAddresses)
	netmailStore.Receive("Avon", "954:700/1", sysop.ID, "sysop", "", "Hi", "\x01INTL 954:700/14 954:700/1\n\x01MSGID: 954:700/1 5\nHello", time.Now(), false)

	copies, err := poster.pointNetmailCopies(netmailStore, readerHobby)
	if err != nil {
		t.Fatal(err)
	}
	if len(copies) != 1 || copies[0].ToAddress != "954:700/14.1" || strings.Contains(copies[0].Body, "INTL") {
		t.Fatalf("copies = %+v, want one to the HobbyNet point without the old INTL", copies)
	}
	b := &outboundBundle{packetName: "x.pkt", netmailCopies: copies, point: true, pointHost: "fidomail"}
	if err := b.markSent(&Result{}, []string{"x.pkt"}, mustAddr(t, "954:700/14.1"), netmailStore, nil, nil); err != nil {
		t.Fatal(err)
	}
	if again, _ := poster.pointNetmailCopies(netmailStore, readerHobby); len(again) != 0 {
		t.Fatalf("copied twice: %+v", again)
	}
	if inbox, _ := netmailStore.Inbox(sysop.ID); len(inbox) != 1 {
		t.Fatalf("the copy must leave the original in the inbox: %+v", inbox)
	}
}

func TestPostAsUnknownUserFails(t *testing.T) {
	_, _, _, users, _, _ := newTestStoresWithRobot(t)
	if _, err := newPointPoster(readerFsx, pointUplinks, users, pointOurAddresses); err == nil {
		t.Fatal("a point posting as a user that doesn't exist was accepted")
	}
	if p, err := newPointPoster(fsxHub, pointUplinks, users, pointOurAddresses); p != nil || err != nil {
		t.Fatalf("a hub got a poster: %+v, %v", p, err)
	}
}
