package tosser

import (
	"bytes"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/areafix"
	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/mail"
)

// A reader app as a point of 21:3/194, in two networks.
var (
	pointOurAddresses = []string{"21:3/194@fsxnet", "954:700/14@hobbynet"}
	fsxHub            = config.BinkpUplink{Address: "21:3/100", Host: "hub.fsx:24554", Network: "fsxNet"}
	readerFsx         = config.BinkpUplink{Address: "21:3/194.1", Host: "fidomail", Network: "fsxNet", Downlink: true, Hold: true}
	readerHobby       = config.BinkpUplink{Address: "954:700/14.1", Host: "fidomail", Network: "HobbyNet", Downlink: true, Hold: true}
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

func TestAreafixStopsAtTheTearline(t *testing.T) {
	list, changes := parseAreafixCommands("+FSX_GEN\n%LIST\n\n--- FidoMailMobile/0.1.12+94 (iOS)\n * Origin: iPad (21:3/194.1)\n-SHOULD_NOT_COUNT\n")
	if !list || len(changes) != 1 || changes[0].Tag != "FSX_GEN" {
		t.Fatalf("list=%v changes=%+v, want %%LIST and +FSX_GEN only", list, changes)
	}
}
