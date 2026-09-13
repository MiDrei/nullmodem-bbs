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

func newTestStores(t *testing.T) (*netmail.Store, *user.Store) {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return netmail.NewStore(sqlDB), user.NewStore(sqlDB)
}

func TestPollSendsPendingNetmailAndMarksSent(t *testing.T) {
	netmailStore, users := newTestStores(t)
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
	}, nil, netmailStore, users)
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
	netmailStore, users := newTestStores(t)
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
	}, nil, netmailStore, users)
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
	netmailStore, users := newTestStores(t)
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
	}, nil, netmailStore, users)
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
	netmailStore, users := newTestStores(t)
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
	}, nil, netmailStore, users)
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
	netmailStore, users := newTestStores(t)
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
	}, nil, netmailStore, users)
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

func TestPollQueuesInboundNetmailForUnresolvedRecipient(t *testing.T) {
	netmailStore, users := newTestStores(t)

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
	}, nil, netmailStore, users)
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
	netmailStore, users := newTestStores(t)
	if _, err := Poll(context.Background(), []string{"not-an-address"}, config.BinkpUplink{Host: "127.0.0.1:1"}, nil, netmailStore, users); err == nil {
		t.Fatal("Poll with an invalid own FTN address: want error, got nil")
	}
}

func TestPollRejectsNoOwnAddresses(t *testing.T) {
	netmailStore, users := newTestStores(t)
	if _, err := Poll(context.Background(), nil, config.BinkpUplink{Host: "127.0.0.1:1"}, nil, netmailStore, users); err == nil {
		t.Fatal("Poll with no own FTN addresses: want error, got nil")
	}
}

func TestPollPresentsAllConfiguredAKAsToUplink(t *testing.T) {
	netmailStore, users := newTestStores(t)

	addr, done := runFakeUplink(t, binkp.Config{
		OurAddresses: []string{"21:3/194"},
	})

	// A point reachable through the same uplink under two different
	// FTN networks (see the 954:700/14 AKA this feature was built
	// for) must present both addresses in the same BinkP session.
	_, err := Poll(context.Background(), []string{"21:3/194.1", "954:700/14"}, config.BinkpUplink{
		Address: "21:3/194",
		Host:    addr,
	}, nil, netmailStore, users)
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
	netmailStore, users := newTestStores(t)
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

	if _, err := Poll(context.Background(), []string{"21:3/194.1"}, config.BinkpUplink{Address: "21:3/194", Host: addr}, nil, netmailStore, users); err == nil {
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
