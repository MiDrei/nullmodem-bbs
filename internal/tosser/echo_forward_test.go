package tosser

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"git.maik.ch/swissmaik/nullmodem/internal/areafix"
	"git.maik.ch/swissmaik/nullmodem/internal/binkp"
	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/mail"
	"git.maik.ch/swissmaik/nullmodem/internal/message"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

func TestRoutedOutboundEchoForwardReturnsSubscribedAreaMessages(t *testing.T) {
	_, messages, _, users, echoSubs, _ := newTestStoresWithRobot(t)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := messages.CreateArea("FSX_GEN", "fsxNet General", "", "fsxNet", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	if _, err := messages.PostMessage(area.ID, alice.ID, "All", "Hi", "hello"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if err := echoSubs.Request(downlinkUplink.Host, "FSX_GEN", areafix.Inbound); err != nil {
		t.Fatalf("Request: %v", err)
	}

	out, err := RoutedOutboundEchoForward(messages, echoSubs, downlinkUplink)
	if err != nil {
		t.Fatalf("RoutedOutboundEchoForward: %v", err)
	}
	if len(out) != 1 || out[0].AreaTag != "FSX_GEN" || out[0].Subject != "Hi" {
		t.Fatalf("RoutedOutboundEchoForward = %+v, want the one message in FSX_GEN", out)
	}
}

func TestRoutedOutboundEchoForwardSkipsMessagesAlreadySeenByTarget(t *testing.T) {
	_, messages, _, users, echoSubs, _ := newTestStoresWithRobot(t)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := messages.CreateArea("FSX_GEN", "fsxNet General", "", "fsxNet", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	posted, err := messages.PostMessage(area.ID, alice.ID, "All", "Hi", "hello")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if err := echoSubs.Request(downlinkUplink.Host, "FSX_GEN", areafix.Inbound); err != nil {
		t.Fatalf("Request: %v", err)
	}
	// downlinkUplink's own address is 21:3/100 -- mark it as already
	// having received this message.
	if err := messages.MarkSeenBy(posted.ID, "3/100"); err != nil {
		t.Fatalf("MarkSeenBy: %v", err)
	}

	out, err := RoutedOutboundEchoForward(messages, echoSubs, downlinkUplink)
	if err != nil {
		t.Fatalf("RoutedOutboundEchoForward: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("RoutedOutboundEchoForward = %+v, want empty -- target already has this message", out)
	}
}

func TestRoutedOutboundEchoForwardSkipsUnsubscribedAreas(t *testing.T) {
	_, messages, _, users, echoSubs, _ := newTestStoresWithRobot(t)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := messages.CreateArea("FSX_GEN", "fsxNet General", "", "fsxNet", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	if _, err := messages.PostMessage(area.ID, alice.ID, "All", "Hi", "hello"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	// No subscription recorded at all.

	out, err := RoutedOutboundEchoForward(messages, echoSubs, downlinkUplink)
	if err != nil {
		t.Fatalf("RoutedOutboundEchoForward: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("RoutedOutboundEchoForward = %+v, want empty -- target has no subscription to this area", out)
	}
}

func TestRoutedOutboundEchoForwardSkipsSubscriptionForDeletedArea(t *testing.T) {
	_, messages, _, _, echoSubs, _ := newTestStoresWithRobot(t)
	if err := echoSubs.Request(downlinkUplink.Host, "GHOST_AREA", areafix.Inbound); err != nil {
		t.Fatalf("Request: %v", err)
	}

	out, err := RoutedOutboundEchoForward(messages, echoSubs, downlinkUplink)
	if err != nil {
		t.Fatalf("RoutedOutboundEchoForward: %v, want a subscription for a nonexistent area to be skipped, not an error", err)
	}
	if len(out) != 0 {
		t.Fatalf("RoutedOutboundEchoForward = %+v, want empty", out)
	}
}

func TestRoutedOutboundEchoForwardIncludesRemoteOriginMessagesToo(t *testing.T) {
	_, messages, _, _, echoSubs, _ := newTestStoresWithRobot(t)
	area, err := messages.CreateArea("FSX_GEN", "fsxNet General", "", "fsxNet", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	if _, _, err := messages.ReceiveEcho(area.ID, "Remote Author", "Re: hi", "hello from upstream", "21:3/1 deadbeef", time.Now()); err != nil {
		t.Fatalf("ReceiveEcho: %v", err)
	}
	if err := echoSubs.Request(downlinkUplink.Host, "FSX_GEN", areafix.Inbound); err != nil {
		t.Fatalf("Request: %v", err)
	}

	out, err := RoutedOutboundEchoForward(messages, echoSubs, downlinkUplink)
	if err != nil {
		t.Fatalf("RoutedOutboundEchoForward: %v", err)
	}
	if len(out) != 1 || out[0].MsgID != "21:3/1 deadbeef" {
		t.Fatalf("RoutedOutboundEchoForward = %+v, want the remote-origin message with its preserved MsgID", out)
	}
}

// TestPollForwardsEchomailToSubscribedDownlinkAndMarksSeenBy is an
// end-to-end regression test through Poll itself: a downlink with an
// active inbound Areafix subscription must receive a copy of a
// locally-posted message in that area, and the message's own SEEN-BY
// must afterward list the downlink so it isn't sent again.
func TestPollForwardsEchomailToSubscribedDownlinkAndMarksSeenBy(t *testing.T) {
	netmailStore, messages, _, users, echoSubs, fileSubs := newTestStoresWithRobot(t)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := messages.CreateArea("FSX_GEN", "fsxNet General", "", "fsxNet", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	posted, err := messages.PostMessage(area.ID, alice.ID, "All", "Hi downlink", "hello there")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	var mu sync.Mutex
	var received []byte
	addr, done := runFakeUplink(t, binkp.Config{
		OurAddresses: []string{downlinkUplink.Address},
		ReceiveFile: func(f binkp.InboundFile, r io.Reader) error {
			data, err := io.ReadAll(r)
			mu.Lock()
			received = data
			mu.Unlock()
			return err
		},
	})

	// The subscription is keyed by uplink_host (see areafix.EchoStore),
	// which must match whatever Host Poll is actually asked to dial --
	// the fake listener's dynamic address here, not downlinkUplink's
	// own placeholder Host.
	dial := downlinkUplink
	dial.Host = addr
	if err := echoSubs.Request(dial.Host, "FSX_GEN", areafix.Inbound); err != nil {
		t.Fatalf("Request: %v", err)
	}

	robot := &RobotConfig{
		OurAddresses: []string{"21:3/194.1"},
		Uplinks:      []config.BinkpUplink{dial},
		EchoStore:    echoSubs,
		FileStore:    fileSubs,
	}

	res, err := Poll(context.Background(), []string{"21:3/194.1"}, "Test BBS", dial, []config.BinkpUplink{dial}, netmailStore, messages, users, robot, nil)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if out := <-done; out.err != nil {
		t.Fatalf("fake uplink answerer error: %v", out.err)
	}
	if res.ForwardedEcho != 1 {
		t.Fatalf("Result.ForwardedEcho = %d, want 1", res.ForwardedEcho)
	}

	mu.Lock()
	pkt := received
	mu.Unlock()
	if len(pkt) == 0 {
		t.Fatal("downlink never received the outbound packet")
	}
	p, err := mail.ReadPacket(bytes.NewReader(pkt))
	if err != nil {
		t.Fatalf("parsing sent packet: %v", err)
	}
	if len(p.Messages) != 1 {
		t.Fatalf("sent packet has %d messages, want 1", len(p.Messages))
	}
	msg := p.Messages[0]
	if !strings.HasPrefix(msg.Body, "AREA:FSX_GEN") {
		t.Fatalf("sent message body = %q, want it to start with AREA:FSX_GEN", msg.Body)
	}
	if !strings.Contains(msg.Body, "hello there") {
		t.Fatalf("sent message body = %q, want the original text included", msg.Body)
	}

	reloaded, err := messages.MessageByID(posted.ID)
	if err != nil {
		t.Fatalf("MessageByID: %v", err)
	}
	if !message.SeenByNetNodes(reloaded.Body)["3/100"] {
		t.Fatalf("stored body = %q, want SEEN-BY to now list the downlink (3/100)", reloaded.Body)
	}

	// A second Poll must not resend it -- the downlink is already
	// marked seen-by.
	out2, err := RoutedOutboundEchoForward(messages, echoSubs, dial)
	if err != nil {
		t.Fatalf("RoutedOutboundEchoForward after send: %v", err)
	}
	if len(out2) != 0 {
		t.Fatalf("RoutedOutboundEchoForward after send = %+v, want empty (already delivered)", out2)
	}
}
