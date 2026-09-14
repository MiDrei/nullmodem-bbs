package tosser

import (
	"bytes"
	"context"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"git.maik.ch/swissmaik/nullmodem/internal/binkp"
	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/db"
	"git.maik.ch/swissmaik/nullmodem/internal/mail"
	"git.maik.ch/swissmaik/nullmodem/internal/message"
	"git.maik.ch/swissmaik/nullmodem/internal/netmail"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

// runFakeUplink starts a goroutine answering exactly one BinkP session
// with ansCfg, over a real TCP loopback listener (see internal/binkp's
// own runPair test helper for why a real socket is needed instead of
// net.Pipe). It returns the "host:port" to dial and a done channel
// that receives the answerer's Result/error once the session ends.
func runFakeUplink(t *testing.T, ansCfg binkp.Config) (addr string, done <-chan ansOutcome) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	outcome := make(chan ansOutcome, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			outcome <- ansOutcome{err: err}
			return
		}
		defer conn.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		res, err := binkp.Answer(ctx, conn, ansCfg)
		outcome <- ansOutcome{result: res, err: err}
	}()
	return ln.Addr().String(), outcome
}

type ansOutcome struct {
	result *binkp.Result
	err    error
}

func newTestStores(t *testing.T) (*netmail.Store, *message.Store, *user.Store) {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return netmail.NewStore(sqlDB), message.NewStore(sqlDB), user.NewStore(sqlDB)
}

func TestPollSendsPendingNetmailAndMarksSent(t *testing.T) {
	netmailStore, messages, users := newTestStores(t)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	if _, err := netmailStore.Send(alice.ID, "21:3/194.1", 0, "Mike Dreier", "21:3/194", "Hi remote", "hello there", false); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var mu sync.Mutex
	var received []byte
	addr, done := runFakeUplink(t, binkp.Config{
		OurAddresses: []string{"21:3/194"},
		ReceiveFile: func(f binkp.InboundFile, r io.Reader) error {
			data, err := io.ReadAll(r)
			mu.Lock()
			received = data
			mu.Unlock()
			return err
		},
	})

	res, err := Poll(context.Background(), []string{"21:3/194.1"}, config.BinkpUplink{
		Address: "21:3/194",
		Host:    addr,
	}, nil, netmailStore, messages, users)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if out := <-done; out.err != nil {
		t.Fatalf("fake uplink answerer error: %v", out.err)
	}

	if res.Sent != 1 {
		t.Fatalf("Result.Sent = %d, want 1", res.Sent)
	}

	pending, err := netmailStore.PendingOutbound()
	if err != nil {
		t.Fatalf("PendingOutbound: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("PendingOutbound after Poll = %+v, want empty (message should be marked sent)", pending)
	}

	mu.Lock()
	pkt := received
	mu.Unlock()
	if len(pkt) == 0 {
		t.Fatal("uplink never received the outbound packet")
	}
	p, err := mail.ReadPacket(bytes.NewReader(pkt))
	if err != nil {
		t.Fatalf("parsing sent packet: %v", err)
	}
	if len(p.Messages) != 1 {
		t.Fatalf("sent packet has %d messages, want 1", len(p.Messages))
	}
	msg := p.Messages[0]
	// Our own address has a point (21:3/194.1), so internal/mail
	// prepends an FMPT kludge to the body -- see internal/mail's
	// addressingKludges.
	if msg.Subject != "Hi remote" || !strings.HasSuffix(msg.Body, "hello there") {
		t.Fatalf("sent message = %+v, want Subject %q Body ending in %q", msg, "Hi remote", "hello there")
	}
	if msg.ToName != "Mike Dreier" {
		t.Fatalf("sent message ToName = %q, want %q", msg.ToName, "Mike Dreier")
	}
	if msg.FromName != "alice" {
		t.Fatalf("sent message FromName = %q, want %q", msg.FromName, "alice")
	}
}

func TestPollReceivesInboundNetmailForLocalUser(t *testing.T) {
	netmailStore, messages, users := newTestStores(t)
	bob, err := users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}

	var buf bytes.Buffer
	w, err := mail.NewWriter(&buf, mail.PacketHeader{
		OrigAddr: mail.Address{Zone: 21, Net: 3, Node: 194},
		DestAddr: mail.Address{Zone: 21, Net: 3, Node: 194, Point: 1},
		Created:  time.Now(),
	})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.WriteMessage(mail.Message{
		ToName:   "bob",
		FromName: "Mike Dreier",
		Subject:  "Hello from FidoNet",
		Body:     "hi bob",
	}); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	addr, done := runFakeUplink(t, binkp.Config{
		OurAddresses: []string{"21:3/194"},
		OutboundFiles: []binkp.OutboundFile{
			{Name: "12345678.pkt", Size: int64(buf.Len()), ModTime: time.Now(), Data: &buf},
		},
	})

	res, err := Poll(context.Background(), []string{"21:3/194.1"}, config.BinkpUplink{
		Address: "21:3/194",
		Host:    addr,
	}, nil, netmailStore, messages, users)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if out := <-done; out.err != nil {
		t.Fatalf("fake uplink answerer error: %v", out.err)
	}

	if res.Received != 1 {
		t.Fatalf("Result.Received = %d, want 1", res.Received)
	}

	inbox, err := netmailStore.Inbox(bob.ID)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(inbox) != 1 {
		t.Fatalf("bob's inbox has %d messages, want 1", len(inbox))
	}
	if inbox[0].FromName != "Mike Dreier" || inbox[0].Subject != "Hello from FidoNet" {
		t.Fatalf("delivered message = %+v, want From %q Subject %q", inbox[0], "Mike Dreier", "Hello from FidoNet")
	}
	if !inbox[0].IsFromRemote() {
		t.Fatal("expected IsFromRemote() = true for mail received from the uplink")
	}
}

func TestPollStampsPacketPasswordOnOutboundPacket(t *testing.T) {
	netmailStore, messages, users := newTestStores(t)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	if _, err := netmailStore.Send(alice.ID, "21:3/194.1", 0, "Mike Dreier", "21:3/194", "Hi", "body", false); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var mu sync.Mutex
	var received []byte
	addr, done := runFakeUplink(t, binkp.Config{
		OurAddresses: []string{"21:3/194"},
		ReceiveFile: func(f binkp.InboundFile, r io.Reader) error {
			data, err := io.ReadAll(r)
			mu.Lock()
			received = data
			mu.Unlock()
			return err
		},
	})

	_, err = Poll(context.Background(), []string{"21:3/194.1"}, config.BinkpUplink{
		Address:        "21:3/194",
		Host:           addr,
		PacketPassword: "pktpass",
	}, nil, netmailStore, messages, users)
	if err != nil {
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
	if p.Header.Password != "pktpass" {
		t.Fatalf("sent packet header password = %q, want %q", p.Header.Password, "pktpass")
	}
}

func TestPollRejectsInboundPacketWithWrongPassword(t *testing.T) {
	netmailStore, messages, users := newTestStores(t)
	if _, err := users.Register("bob", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register bob: %v", err)
	}

	var buf bytes.Buffer
	w, err := mail.NewWriter(&buf, mail.PacketHeader{
		OrigAddr: mail.Address{Zone: 21, Net: 3, Node: 194},
		DestAddr: mail.Address{Zone: 21, Net: 3, Node: 194, Point: 1},
		Created:  time.Now(),
		Password: "wrongpw",
	})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.WriteMessage(mail.Message{ToName: "bob", FromName: "Mike Dreier", Subject: "Hi", Body: "hi"}); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	addr, done := runFakeUplink(t, binkp.Config{
		OurAddresses: []string{"21:3/194"},
		OutboundFiles: []binkp.OutboundFile{
			{Name: "12345678.pkt", Size: int64(buf.Len()), ModTime: time.Now(), Data: &buf},
		},
	})

	_, err = Poll(context.Background(), []string{"21:3/194.1"}, config.BinkpUplink{
		Address:        "21:3/194",
		Host:           addr,
		PacketPassword: "rightpw",
	}, nil, netmailStore, messages, users)
	if err == nil {
		t.Fatal("Poll with a wrong inbound packet password: want error, got nil")
	}
	<-done

	inbox, err := netmailStore.Inbox(1)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(inbox) != 0 {
		t.Fatalf("Inbox after a rejected packet = %+v, want no messages stored", inbox)
	}
}

func TestPollAcceptsInboundPacketWithMatchingPassword(t *testing.T) {
	netmailStore, messages, users := newTestStores(t)
	bob, err := users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}

	var buf bytes.Buffer
	w, err := mail.NewWriter(&buf, mail.PacketHeader{
		OrigAddr: mail.Address{Zone: 21, Net: 3, Node: 194},
		DestAddr: mail.Address{Zone: 21, Net: 3, Node: 194, Point: 1},
		Created:  time.Now(),
		Password: "rightpw",
	})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.WriteMessage(mail.Message{ToName: "bob", FromName: "Mike Dreier", Subject: "Hi", Body: "hi"}); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	addr, done := runFakeUplink(t, binkp.Config{
		OurAddresses: []string{"21:3/194"},
		OutboundFiles: []binkp.OutboundFile{
			{Name: "12345678.pkt", Size: int64(buf.Len()), ModTime: time.Now(), Data: &buf},
		},
	})

	res, err := Poll(context.Background(), []string{"21:3/194.1"}, config.BinkpUplink{
		Address:        "21:3/194",
		Host:           addr,
		PacketPassword: "rightpw",
	}, nil, netmailStore, messages, users)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if out := <-done; out.err != nil {
		t.Fatalf("fake uplink answerer error: %v", out.err)
	}
	if res.Received != 1 {
		t.Fatalf("Result.Received = %d, want 1", res.Received)
	}

	inbox, err := netmailStore.Inbox(bob.ID)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(inbox) != 1 {
		t.Fatalf("bob's inbox has %d messages, want 1", len(inbox))
	}
}

// TestPollAcceptsInboundPacketPasswordCaseInsensitively locks in a
// real interop fix: a live uplink stamped its packets with an
// all-uppercase password while our configured value used mixed case
// -- FTN packet passwords are conventionally compared case-
// insensitively, same as most FTN passwords.
func TestPollAcceptsInboundPacketPasswordCaseInsensitively(t *testing.T) {
	netmailStore, messages, users := newTestStores(t)
	if _, err := users.Register("bob", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register bob: %v", err)
	}

	var buf bytes.Buffer
	w, err := mail.NewWriter(&buf, mail.PacketHeader{
		OrigAddr: mail.Address{Zone: 21, Net: 3, Node: 194},
		DestAddr: mail.Address{Zone: 21, Net: 3, Node: 194, Point: 1},
		Created:  time.Now(),
		Password: "RIGHTPW",
	})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.WriteMessage(mail.Message{ToName: "bob", FromName: "Mike Dreier", Subject: "Hi", Body: "hi"}); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	addr, done := runFakeUplink(t, binkp.Config{
		OurAddresses: []string{"21:3/194"},
		OutboundFiles: []binkp.OutboundFile{
			{Name: "12345678.pkt", Size: int64(buf.Len()), ModTime: time.Now(), Data: &buf},
		},
	})

	res, err := Poll(context.Background(), []string{"21:3/194.1"}, config.BinkpUplink{
		Address:        "21:3/194",
		Host:           addr,
		PacketPassword: "rightpw", // mixed/lower case configured, uppercase on the wire
	}, nil, netmailStore, messages, users)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if out := <-done; out.err != nil {
		t.Fatalf("fake uplink answerer error: %v", out.err)
	}
	if res.Received != 1 {
		t.Fatalf("Result.Received = %d, want 1", res.Received)
	}
}

// TestPollAcceptsInboundPacketPasswordFromASiblingUplinkOnTheSameHost
// locks in a real interop fix: a single hub can serve more than one of
// our AKAs/networks over what our config models as separate uplink
// entries (one per network, each with its own packet password) but is
// really one physical link -- observed live where one host identified
// itself in the handshake as both our fsxNet and HobbyNet uplink. A
// file packed under the *other* configured uplink's password, as long
// as it shares the same Host, must still be accepted rather than
// aborting the whole session.
func TestPollAcceptsInboundPacketPasswordFromASiblingUplinkOnTheSameHost(t *testing.T) {
	netmailStore, messages, users := newTestStores(t)

	var buf bytes.Buffer
	// Both passwords below are kept to 8 characters or fewer:
	// internal/mail's packet header password field is a fixed 8 bytes
	// (see passwordField), so anything longer would silently truncate
	// on the wire and this test would be asserting against a value
	// that was never actually sent.
	w, err := mail.NewWriter(&buf, mail.PacketHeader{
		OrigAddr: mail.Address{Zone: 21, Net: 3, Node: 194},
		DestAddr: mail.Address{Zone: 21, Net: 3, Node: 195},
		Created:  time.Now(),
		Password: "hobbynet",
	})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.WriteMessage(mail.Message{
		ToName:   "All",
		FromName: "Someone",
		Subject:  "Hi from the other network",
		Body:     "AREA:HOBBY_GENERAL\rbody text",
	}); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	addr, done := runFakeUplink(t, binkp.Config{
		OurAddresses: []string{"21:3/194"},
		OutboundFiles: []binkp.OutboundFile{
			{Name: "12345678.pkt", Size: int64(buf.Len()), ModTime: time.Now(), Data: &buf},
		},
	})

	fsxnetUplink := config.BinkpUplink{
		Address:        "21:3/100",
		Host:           addr,
		PacketPassword: "fsxnetpw",
	}
	hobbynetUplink := config.BinkpUplink{
		Address:        "954:700/1",
		Host:           addr,
		PacketPassword: "hobbynet",
	}

	res, err := Poll(context.Background(), []string{"21:3/194.1"}, fsxnetUplink,
		[]config.BinkpUplink{fsxnetUplink, hobbynetUplink}, netmailStore, messages, users)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if out := <-done; out.err != nil {
		t.Fatalf("fake uplink answerer error: %v", out.err)
	}
	if res.ReceivedEcho != 1 {
		t.Fatalf("Result.ReceivedEcho = %d, want 1", res.ReceivedEcho)
	}
}

func TestPollQueuesInboundNetmailForUnresolvedRecipient(t *testing.T) {
	netmailStore, messages, users := newTestStores(t)

	var buf bytes.Buffer
	w, err := mail.NewWriter(&buf, mail.PacketHeader{
		OrigAddr: mail.Address{Zone: 21, Net: 3, Node: 194},
		DestAddr: mail.Address{Zone: 21, Net: 3, Node: 194, Point: 1},
		Created:  time.Now(),
	})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.WriteMessage(mail.Message{
		ToName:   "nosuchuser",
		FromName: "Mike Dreier",
		Subject:  "Hello",
		Body:     "hi",
	}); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	addr, done := runFakeUplink(t, binkp.Config{
		OurAddresses: []string{"21:3/194"},
		OutboundFiles: []binkp.OutboundFile{
			{Name: "12345678.pkt", Size: int64(buf.Len()), ModTime: time.Now(), Data: &buf},
		},
	})

	res, err := Poll(context.Background(), []string{"21:3/194.1"}, config.BinkpUplink{
		Address: "21:3/194",
		Host:    addr,
	}, nil, netmailStore, messages, users)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if out := <-done; out.err != nil {
		t.Fatalf("fake uplink answerer error: %v", out.err)
	}
	if res.Received != 1 {
		t.Fatalf("Result.Received = %d, want 1", res.Received)
	}
}

func TestPollRejectsInvalidOwnAddress(t *testing.T) {
	netmailStore, messages, users := newTestStores(t)
	if _, err := Poll(context.Background(), []string{"not-an-address"}, config.BinkpUplink{Host: "127.0.0.1:1"}, nil, netmailStore, messages, users); err == nil {
		t.Fatal("Poll with an invalid own FTN address: want error, got nil")
	}
}

func TestPollRejectsNoOwnAddresses(t *testing.T) {
	netmailStore, messages, users := newTestStores(t)
	if _, err := Poll(context.Background(), nil, config.BinkpUplink{Host: "127.0.0.1:1"}, nil, netmailStore, messages, users); err == nil {
		t.Fatal("Poll with no own FTN addresses: want error, got nil")
	}
}

func TestPollPresentsAllConfiguredAKAsToUplink(t *testing.T) {
	netmailStore, messages, users := newTestStores(t)

	addr, done := runFakeUplink(t, binkp.Config{
		OurAddresses: []string{"21:3/194"},
	})

	// A point reachable through the same uplink under two different
	// FTN networks (see the 954:700/14 AKA this feature was built
	// for) must present both addresses in the same BinkP session.
	_, err := Poll(context.Background(), []string{"21:3/194.1", "954:700/14"}, config.BinkpUplink{
		Address: "21:3/194",
		Host:    addr,
	}, nil, netmailStore, messages, users)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	out := <-done
	if out.err != nil {
		t.Fatalf("fake uplink answerer error: %v", out.err)
	}

	want := []string{"21:3/194.1", "954:700/14"}
	got := out.result.RemoteAddresses
	if len(got) != len(want) {
		t.Fatalf("uplink saw AKAs %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("uplink saw AKAs %v, want %v", got, want)
		}
	}
}

func TestPollErrorsWhenUplinkUnreachable(t *testing.T) {
	netmailStore, messages, users := newTestStores(t)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	if _, err := netmailStore.Send(alice.ID, "21:3/194.1", 0, "Someone", "21:3/194", "Hi", "body", false); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Nothing is listening on this port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	if _, err := Poll(context.Background(), []string{"21:3/194.1"}, config.BinkpUplink{Address: "21:3/194", Host: addr}, nil, netmailStore, messages, users); err == nil {
		t.Fatal("Poll against an unreachable uplink: want error, got nil")
	}

	pending, err := netmailStore.PendingOutbound()
	if err != nil {
		t.Fatalf("PendingOutbound: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("PendingOutbound after a failed poll = %+v, want the message still queued", pending)
	}
}

func TestPollTossesEchomailIntoAutoCreatedPendingArea(t *testing.T) {
	netmailStore, messages, users := newTestStores(t)

	var buf bytes.Buffer
	// No Point on either address: unlike netmail, echomail doesn't
	// carry per-message zone/point kludges (it's always routed via
	// its AREA tag, addressed to "All"), so internal/mail won't
	// prepend an FMPT/INTL line before our hand-written AREA line --
	// matching a real inbound echomail packet's shape.
	w, err := mail.NewWriter(&buf, mail.PacketHeader{
		OrigAddr: mail.Address{Zone: 21, Net: 3, Node: 194},
		DestAddr: mail.Address{Zone: 21, Net: 3, Node: 195},
		Created:  time.Now(),
	})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.WriteMessage(mail.Message{
		ToName:   "All",
		FromName: "Geri Atricks",
		Subject:  "Re: Immortal Barons",
		Body:     "AREA:FSXNET_GENERAL\r\x01MSGID: 21:3/235 abcdef01\rhello area\r",
	}); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	addr, done := runFakeUplink(t, binkp.Config{
		OurAddresses: []string{"21:3/194"},
		OutboundFiles: []binkp.OutboundFile{
			{Name: "12345678.pkt", Size: int64(buf.Len()), ModTime: time.Now(), Data: &buf},
		},
	})

	res, err := Poll(context.Background(), []string{"21:3/194.1"}, config.BinkpUplink{
		Address: "21:3/194",
		Host:    addr,
	}, nil, netmailStore, messages, users)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if out := <-done; out.err != nil {
		t.Fatalf("fake uplink answerer error: %v", out.err)
	}

	if res.ReceivedEcho != 1 {
		t.Fatalf("Result.ReceivedEcho = %d, want 1", res.ReceivedEcho)
	}
	if res.Received != 0 {
		t.Fatalf("Result.Received (netmail) = %d, want 0 -- this was echomail", res.Received)
	}

	// Netmail must never see this message.
	pending, err := netmailStore.PendingOutbound()
	if err != nil {
		t.Fatalf("PendingOutbound: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("PendingOutbound = %+v, want empty -- echomail must not land in netmail", pending)
	}

	area, err := messages.AreaByTag("FSXNET_GENERAL")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if !area.Pending {
		t.Fatal("auto-created area Pending = false, want true until the sysop approves it")
	}

	// Invisible to the BBS until approved.
	all, err := messages.AllAreas()
	if err != nil {
		t.Fatalf("AllAreas: %v", err)
	}
	for _, a := range all {
		if a.Tag == "FSXNET_GENERAL" {
			t.Fatal("AllAreas included the still-pending auto-created area")
		}
	}

	msgs, err := messages.ListMessages(area.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("ListMessages = %+v, want 1 message", msgs)
	}
	got := msgs[0]
	if got.FromName != "Geri Atricks" || got.Subject != "Re: Immortal Barons" {
		t.Fatalf("tossed message = %+v, want From %q Subject %q", got, "Geri Atricks", "Re: Immortal Barons")
	}
	if !got.IsFromRemote() {
		t.Fatal("expected IsFromRemote() = true for a tossed echomail message")
	}
	if strings.Contains(got.Body, "\x01") {
		t.Fatalf("tossed message body still contains kludge lines: %q", got.Body)
	}
	if !strings.Contains(got.Body, "hello area") {
		t.Fatalf("tossed message body lost its real text: %q", got.Body)
	}
}

// TestPollSkipsEchomailAlreadyTossedUnderTheSameMsgID locks in a real
// interop fix: a hub that closed its BinkP connection right after
// sending its last file (see internal/binkp's receiveOneFile) instead
// of waiting for our M_GOT resends the same message on its next
// session -- FTS-1026 acknowledges this exact risk. A second poll
// delivering byte-identical mail (same MSGID) must not store it twice
// or count it in ReceivedEcho again.
// TestPollSkipsNonPacketInboundFilesInsteadOfAbortingTheSession locks
// in a real interop fix: a live hub bundled a .tic file-echo
// announcement into the very same session as an ordinary mail packet
// (a .tic file's OPT-advertised place in binkp is entirely different
// from FTS-0001's -- it's a plain key/value text file, not a mail
// packet). Feeding it to the packet parser failed hard ("unsupported
// packet version") and aborted the whole session, discarding the
// Result for mail already tossed earlier in it -- even though the DB
// writes themselves had already happened and stuck. The fix must
// recognize a non-.pkt file, skip it (report it in SkippedFiles), and
// still return the mail packet's own results successfully.
func TestPollSkipsNonPacketInboundFilesInsteadOfAbortingTheSession(t *testing.T) {
	netmailStore, messages, users := newTestStores(t)

	var buf bytes.Buffer
	w, err := mail.NewWriter(&buf, mail.PacketHeader{
		OrigAddr: mail.Address{Zone: 21, Net: 3, Node: 194},
		DestAddr: mail.Address{Zone: 21, Net: 3, Node: 195},
		Created:  time.Now(),
	})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.WriteMessage(mail.Message{
		ToName:   "All",
		FromName: "Geri Atricks",
		Subject:  "Re: Immortal Barons",
		Body:     "AREA:FSXNET_GENERAL\r\x01MSGID: 21:3/235 abcdef01\rhello area\r",
	}); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A .tic file is plain text, nothing like an FTS-0001 packet --
	// enough to make mail.NewReader fail if it's ever handed this.
	ticContent := "Area FSX_GAMES\r\nFile somefile.zip\r\nSize 12345\r\nOrigin 21:3/235\r\n"
	ticBuf := bytes.NewBufferString(ticContent)

	addr, done := runFakeUplink(t, binkp.Config{
		OurAddresses: []string{"21:3/194"},
		OutboundFiles: []binkp.OutboundFile{
			{Name: "12345678.pkt", Size: int64(buf.Len()), ModTime: time.Now(), Data: &buf},
			{Name: "a5d42c40.tic", Size: int64(ticBuf.Len()), ModTime: time.Now(), Data: ticBuf},
		},
	})

	res, err := Poll(context.Background(), []string{"21:3/194.1"}, config.BinkpUplink{
		Address: "21:3/194",
		Host:    addr,
	}, nil, netmailStore, messages, users)
	if err != nil {
		t.Fatalf("Poll: %v, want no error despite the non-packet .tic file", err)
	}
	if out := <-done; out.err != nil {
		t.Fatalf("fake uplink answerer error: %v", out.err)
	}

	if res.ReceivedEcho != 1 {
		t.Fatalf("Result.ReceivedEcho = %d, want 1 -- the .pkt file's mail should still be tossed", res.ReceivedEcho)
	}
	if len(res.SkippedFiles) != 1 || res.SkippedFiles[0] != "a5d42c40.tic" {
		t.Fatalf("Result.SkippedFiles = %v, want [a5d42c40.tic]", res.SkippedFiles)
	}

	area, err := messages.AreaByTag("FSXNET_GENERAL")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	msgs, err := messages.ListMessages(area.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("ListMessages = %+v, want 1 message from the .pkt file", msgs)
	}
}

func TestPollSkipsEchomailAlreadyTossedUnderTheSameMsgID(t *testing.T) {
	netmailStore, messages, users := newTestStores(t)

	packet := func() bytes.Buffer {
		var buf bytes.Buffer
		w, err := mail.NewWriter(&buf, mail.PacketHeader{
			OrigAddr: mail.Address{Zone: 21, Net: 3, Node: 194},
			DestAddr: mail.Address{Zone: 21, Net: 3, Node: 195},
			Created:  time.Now(),
		})
		if err != nil {
			t.Fatalf("NewWriter: %v", err)
		}
		if err := w.WriteMessage(mail.Message{
			ToName:   "All",
			FromName: "Geri Atricks",
			Subject:  "Re: Immortal Barons",
			Body:     "AREA:FSXNET_GENERAL\r\x01MSGID: 21:3/235 abcdef01\rhello area\r",
		}); err != nil {
			t.Fatalf("WriteMessage: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		return buf
	}

	poll := func() *Result {
		buf := packet()
		addr, done := runFakeUplink(t, binkp.Config{
			OurAddresses: []string{"21:3/194"},
			OutboundFiles: []binkp.OutboundFile{
				{Name: "12345678.pkt", Size: int64(buf.Len()), ModTime: time.Now(), Data: &buf},
			},
		})
		res, err := Poll(context.Background(), []string{"21:3/194.1"}, config.BinkpUplink{
			Address: "21:3/194",
			Host:    addr,
		}, nil, netmailStore, messages, users)
		if err != nil {
			t.Fatalf("Poll: %v", err)
		}
		if out := <-done; out.err != nil {
			t.Fatalf("fake uplink answerer error: %v", out.err)
		}
		return res
	}

	first := poll()
	if first.ReceivedEcho != 1 {
		t.Fatalf("first poll Result.ReceivedEcho = %d, want 1", first.ReceivedEcho)
	}

	second := poll()
	if second.ReceivedEcho != 0 {
		t.Fatalf("resend poll Result.ReceivedEcho = %d, want 0 -- it's a duplicate MSGID", second.ReceivedEcho)
	}

	area, err := messages.AreaByTag("FSXNET_GENERAL")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	msgs, err := messages.ListMessages(area.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("ListMessages = %+v, want exactly 1 message despite the resend", msgs)
	}
}

func TestPollTossesEchomailIntoExistingApprovedArea(t *testing.T) {
	netmailStore, messages, users := newTestStores(t)
	existing, err := messages.CreateArea("dev", "Development Talk", "", "", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}

	var buf bytes.Buffer
	// No Point (see TestPollTossesEchomailIntoAutoCreatedPendingArea).
	w, err := mail.NewWriter(&buf, mail.PacketHeader{
		OrigAddr: mail.Address{Zone: 21, Net: 3, Node: 194},
		DestAddr: mail.Address{Zone: 21, Net: 3, Node: 195},
		Created:  time.Now(),
	})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.WriteMessage(mail.Message{
		ToName:   "All",
		FromName: "Someone",
		Subject:  "Hi",
		Body:     "AREA:dev\rbody text",
	}); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	addr, done := runFakeUplink(t, binkp.Config{
		OurAddresses: []string{"21:3/194"},
		OutboundFiles: []binkp.OutboundFile{
			{Name: "12345678.pkt", Size: int64(buf.Len()), ModTime: time.Now(), Data: &buf},
		},
	})

	res, err := Poll(context.Background(), []string{"21:3/194.1"}, config.BinkpUplink{
		Address: "21:3/194",
		Host:    addr,
	}, nil, netmailStore, messages, users)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if out := <-done; out.err != nil {
		t.Fatalf("fake uplink answerer error: %v", out.err)
	}
	if res.ReceivedEcho != 1 {
		t.Fatalf("Result.ReceivedEcho = %d, want 1", res.ReceivedEcho)
	}

	reloaded, err := messages.AreaByID(existing.ID)
	if err != nil {
		t.Fatalf("AreaByID: %v", err)
	}
	if reloaded.Pending {
		t.Fatal("an already-existing, already-approved area must not become pending again")
	}
	if reloaded.Name != "Development Talk" {
		t.Fatalf("Name = %q, want the untouched original %q", reloaded.Name, "Development Talk")
	}
}

func TestEchoAreaTagRequiresBareAreaLineNotKludgePrefixed(t *testing.T) {
	// Real-world shape (confirmed against live fsxNet traffic from
	// both Synchronet- and binkd-based systems): AREA is a bare first
	// line with no \x01, unlike every kludge that follows it.
	tag, ok := echoAreaTag("AREA:FSX_GAMING\n\x01TID: clrghouz fcc93214\nsome text")
	if !ok || tag != "FSX_GAMING" {
		t.Fatalf("echoAreaTag() = (%q, %v), want (\"FSX_GAMING\", true)", tag, ok)
	}
}

func TestEchoAreaTagFalseForOrdinaryNetmail(t *testing.T) {
	if _, ok := echoAreaTag("just a normal netmail body\nwith no area line"); ok {
		t.Fatal("echoAreaTag() = true for a body with no AREA line, want false")
	}
	// A \x01-prefixed "AREA:" (not the real convention) must not
	// match either -- only a bare first line counts.
	if _, ok := echoAreaTag("\x01AREA:SHOULDNOTMATCH\ntext"); ok {
		t.Fatal("echoAreaTag() = true for a \\x01-prefixed AREA line, want false (that's not the real convention)")
	}
}

func TestStripLeadingKludgesRemovesBareAreaLineAndFollowingKludges(t *testing.T) {
	body := "AREA:FSX_GAMING\n\x01TID: clrghouz fcc93214\n\x01MSGID: 21:3/235 0ddd24af\nthe actual message text\nmore text"
	got := stripLeadingKludges(body)
	want := "the actual message text\nmore text"
	if got != want {
		t.Fatalf("stripLeadingKludges() = %q, want %q", got, want)
	}
}

// TestAnswerAuthenticatesKnownUplinkAndTossesMail locks in inbound
// BinkP support (a caller dialing us, e.g. a hub pushing mail between
// our own scheduled polls): Answer must recognize a caller by
// matching its M_ADR against configured Uplinks, authenticate with
// that uplink's own Password, and toss whatever it sends using that
// same uplink's PacketPassword -- exactly like Poll does for an
// outbound session, just mirrored.
func TestAnswerAuthenticatesKnownUplinkAndTossesMail(t *testing.T) {
	netmailStore, messages, users := newTestStores(t)

	uplink := config.BinkpUplink{
		Address:        "21:3/100",
		Host:           "unused-for-answer-test",
		Password:       "sess3cret",
		PacketPassword: "pktpw01",
	}

	var buf bytes.Buffer
	w, err := mail.NewWriter(&buf, mail.PacketHeader{
		OrigAddr: mail.Address{Zone: 21, Net: 3, Node: 100},
		DestAddr: mail.Address{Zone: 21, Net: 3, Node: 194},
		Created:  time.Now(),
		Password: "pktpw01",
	})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.WriteMessage(mail.Message{
		ToName:   "All",
		FromName: "Someone",
		Subject:  "Pushed while we weren't polling",
		Body:     "AREA:FSX_PUSHED\rhello from a push\r",
	}); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	type answerOutcome struct {
		res *Result
		err error
	}
	answerCh := make(chan answerOutcome, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			answerCh <- answerOutcome{err: err}
			return
		}
		defer conn.Close()
		res, err := Answer(context.Background(), conn, []string{"21:3/194"}, []config.BinkpUplink{uplink}, netmailStore, messages, users)
		answerCh <- answerOutcome{res: res, err: err}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = binkp.Dial(ctx, ln.Addr().String(), binkp.Config{
		OurAddresses: []string{uplink.Address},
		Password:     uplink.Password,
		OutboundFiles: []binkp.OutboundFile{
			{Name: "12345678.pkt", Size: int64(buf.Len()), ModTime: time.Now(), Data: &buf},
		},
	})
	if err != nil {
		t.Fatalf("caller-side Dial: %v", err)
	}

	out := <-answerCh
	if out.err != nil {
		t.Fatalf("Answer: %v", out.err)
	}
	if out.res.ReceivedEcho != 1 {
		t.Fatalf("Result.ReceivedEcho = %d, want 1", out.res.ReceivedEcho)
	}

	area, err := messages.AreaByTag("FSX_PUSHED")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	msgs, err := messages.ListMessages(area.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Subject != "Pushed while we weren't polling" {
		t.Fatalf("ListMessages = %+v, want the pushed message", msgs)
	}
}

// TestAnswerRejectsCallerNotMatchingAnyConfiguredUplink locks in the
// reject path: a caller whose M_ADR matches none of our configured
// uplinks must be refused, not silently accepted as an open node.
func TestAnswerRejectsCallerNotMatchingAnyConfiguredUplink(t *testing.T) {
	netmailStore, messages, users := newTestStores(t)

	knownUplink := config.BinkpUplink{
		Address:  "21:3/100",
		Host:     "unused-for-answer-test",
		Password: "sess3cret",
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	type answerOutcome struct {
		res *Result
		err error
	}
	answerCh := make(chan answerOutcome, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			answerCh <- answerOutcome{err: err}
			return
		}
		defer conn.Close()
		res, err := Answer(context.Background(), conn, []string{"21:3/194"}, []config.BinkpUplink{knownUplink}, netmailStore, messages, users)
		answerCh <- answerOutcome{res: res, err: err}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, dialErr := binkp.Dial(ctx, ln.Addr().String(), binkp.Config{
		OurAddresses: []string{"9:9/999"},
		Password:     "doesnt-matter",
	})
	if dialErr == nil {
		t.Fatal("caller-side Dial: expected an error for a rejected, unrecognized caller")
	}

	out := <-answerCh
	if out.err == nil {
		t.Fatal("Answer: expected an error for an unrecognized caller")
	}
}
