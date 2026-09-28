package telnet

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

// negotiateAll feeds cmds (each IAC <cmd> <opt>) to a session that has
// already sent its initial offers, and returns everything it replies.
func negotiateAll(t *testing.T, sess *Session, client net.Conn, cmds []byte) []byte {
	t.Helper()
	var replies bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 256)
		for {
			client.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
			n, err := client.Read(buf)
			replies.Write(buf[:n])
			if err != nil {
				return
			}
		}
	}()
	for i := 0; i+2 < len(cmds); i += 3 {
		if err := sess.negotiate(cmds[i+1], cmds[i+2]); err != nil {
			t.Fatalf("negotiate: %v", err)
		}
	}
	<-done
	return replies.Bytes()
}

func initialSession(t *testing.T) (*Session, net.Conn) {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() { server.Close(); client.Close() })
	sess := newSession(server)
	go sess.negotiateInitial()
	io.ReadFull(client, make([]byte, 15)) // the initial offers
	return sess, client
}

// TestNegotiateDoesNotAcknowledgeConfirmations: a client confirming our
// own offers (DO SGA/ECHO/BINARY) and our requests (WILL NAWS/TTYPE)
// gets no reply but the TTYPE request -- answering a confirmation is
// how the endless WILL/DO loop happened.
func TestNegotiateDoesNotAcknowledgeConfirmations(t *testing.T) {
	sess, client := initialSession(t)
	got := negotiateAll(t, sess, client, []byte{
		iac, do, optSGA, iac, do, optEcho, iac, do, optBinary,
		iac, will, optNAWS, iac, will, optTType,
	})
	want := []byte{iac, sb, optTType, 1, iac, se}
	if !bytes.Equal(got, want) {
		t.Fatalf("replies = % x, want only the TTYPE request % x", got, want)
	}
}

// TestNegotiateNaiveClientLoopEnds replays what a client that answers
// every WILL with DO and every DO with WONT sends back, several rounds
// over: after the first round there must be nothing left to answer.
func TestNegotiateNaiveClientLoopEnds(t *testing.T) {
	sess, client := initialSession(t)
	round := []byte{
		iac, do, optSGA, iac, do, optEcho, iac, do, optBinary, // their DO for our WILLs
		iac, wont, optNAWS, iac, wont, optTType, // their WONT for our DOs
	}
	first := negotiateAll(t, sess, client, round)
	if len(first) != 0 {
		t.Fatalf("first round replies = % x, want none", first)
	}
	for i := 0; i < 3; i++ {
		if again := negotiateAll(t, sess, client, round); len(again) != 0 {
			t.Fatalf("round %d replies = % x, want none", i+2, again)
		}
	}
}

// TestNegotiateRefusesUnsupportedOnce: an unsupported option is refused
// once, not again on every repeat of the request.
func TestNegotiateRefusesUnsupportedOnce(t *testing.T) {
	sess, client := initialSession(t)
	got := negotiateAll(t, sess, client, []byte{
		iac, do, optLinemode, iac, do, optLinemode,
		iac, will, optBinary, iac, will, optBinary,
	})
	want := []byte{iac, wont, optLinemode, iac, dont, optBinary}
	if !bytes.Equal(got, want) {
		t.Fatalf("replies = % x, want % x", got, want)
	}
}

// TestNegotiateOptionOffAndOnAgain: switching one of our options off
// is acknowledged, and asking for it again is honoured again.
func TestNegotiateOptionOffAndOnAgain(t *testing.T) {
	sess, client := initialSession(t)
	got := negotiateAll(t, sess, client, []byte{
		iac, dont, optEcho, iac, dont, optEcho, iac, do, optEcho,
	})
	want := []byte{iac, wont, optEcho, iac, will, optEcho}
	if !bytes.Equal(got, want) {
		t.Fatalf("replies = % x, want % x", got, want)
	}
}
