package binkp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// runPair runs originatorCfg and answererCfg against each other over
// a real TCP loopback connection, returning each side's Result/error
// once both have finished.
//
// A real socket is used deliberately instead of net.Pipe: net.Pipe is
// fully synchronous with zero buffering, so a Write blocks until the
// peer's matching Read happens. BinkP's handshake has each side send
// a short burst (M_NUL then M_ADR) before reading anything back --
// over net.Pipe both sides' first Write blocks forever waiting for a
// Read that never comes (each side is itself still writing), a
// deadlock that a real socket's kernel send buffer never triggers.
func runPair(t *testing.T, originatorCfg, answererCfg Config) (origResult, ansResult *Result, origErr, ansErr error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		defer wg.Done()
		answerConn, err := ln.Accept()
		if err != nil {
			ansErr = err
			return
		}
		defer answerConn.Close()
		ansResult, ansErr = runSession(ctx, answerConn, answererCfg, roleAnswerer)
	}()
	go func() {
		defer wg.Done()
		origResult, origErr = Dial(ctx, ln.Addr().String(), originatorCfg)
	}()
	wg.Wait()
	return origResult, ansResult, origErr, ansErr
}

func TestSessionHandshakeAndEmptyTransferNoPassword(t *testing.T) {
	origResult, ansResult, origErr, ansErr := runPair(t,
		Config{OurAddresses: []string{"1:234/56.0"}},
		Config{OurAddresses: []string{"1:234/99.0"}},
	)
	if origErr != nil {
		t.Fatalf("originator error: %v", origErr)
	}
	if ansErr != nil {
		t.Fatalf("answerer error: %v", ansErr)
	}
	if !reflect.DeepEqual(origResult.RemoteAddresses, []string{"1:234/99.0"}) {
		t.Fatalf("originator saw remote addresses %v, want [1:234/99.0]", origResult.RemoteAddresses)
	}
	if !reflect.DeepEqual(ansResult.RemoteAddresses, []string{"1:234/56.0"}) {
		t.Fatalf("answerer saw remote addresses %v, want [1:234/56.0]", ansResult.RemoteAddresses)
	}
}

func TestSessionPasswordAuthSuccessUsesCRAMMD5(t *testing.T) {
	origResult, ansResult, origErr, ansErr := runPair(t,
		Config{OurAddresses: []string{"1:234/56.0"}, Password: "correct horse"},
		Config{OurAddresses: []string{"1:234/99.0"}, Password: "correct horse"},
	)
	if origErr != nil {
		t.Fatalf("originator error: %v", origErr)
	}
	if ansErr != nil {
		t.Fatalf("answerer error: %v", ansErr)
	}
	if origResult == nil || ansResult == nil {
		t.Fatal("expected both sides to complete successfully")
	}
}

func TestSessionPasswordAuthFailureAbortsBothSides(t *testing.T) {
	_, _, origErr, ansErr := runPair(t,
		Config{OurAddresses: []string{"1:234/56.0"}, Password: "wrong password"},
		Config{OurAddresses: []string{"1:234/99.0"}, Password: "correct horse"},
	)
	if origErr == nil {
		t.Fatal("expected the originator to see an authentication failure")
	}
	if ansErr == nil {
		t.Fatal("expected the answerer to see a failed authentication attempt")
	}
}

func TestSessionPlaintextFallbackWhenAnswererHasNoPassword(t *testing.T) {
	// An originator with a password configured still sends it even if
	// the answerer is an open node; the answerer just doesn't ask for
	// or check it, and the session must still complete cleanly.
	origResult, ansResult, origErr, ansErr := runPair(t,
		Config{OurAddresses: []string{"1:234/56.0"}, Password: "unused"},
		Config{OurAddresses: []string{"1:234/99.0"}},
	)
	if origErr != nil {
		t.Fatalf("originator error: %v", origErr)
	}
	if ansErr != nil {
		t.Fatalf("answerer error: %v", ansErr)
	}
	if origResult == nil || ansResult == nil {
		t.Fatal("expected both sides to complete successfully")
	}
}

func TestSessionTransfersFileFromOriginatorToAnswerer(t *testing.T) {
	content := []byte("this is a test netmail packet's bytes")
	var received bytes.Buffer
	var receivedName string

	origResult, ansResult, origErr, ansErr := runPair(t,
		Config{
			OurAddresses: []string{"1:234/56.0"},
			OutboundFiles: []OutboundFile{
				{Name: "0000abcd.pkt", Size: int64(len(content)), ModTime: time.Now(), Data: bytes.NewReader(content)},
			},
		},
		Config{
			OurAddresses: []string{"1:234/99.0"},
			ReceiveFile: func(f InboundFile, r io.Reader) error {
				receivedName = f.Name
				_, err := io.Copy(&received, r)
				return err
			},
		},
	)
	if origErr != nil {
		t.Fatalf("originator error: %v", origErr)
	}
	if ansErr != nil {
		t.Fatalf("answerer error: %v", ansErr)
	}
	if receivedName != "0000abcd.pkt" {
		t.Fatalf("received file name = %q, want %q", receivedName, "0000abcd.pkt")
	}
	if !bytes.Equal(received.Bytes(), content) {
		t.Fatalf("received content = %q, want %q", received.Bytes(), content)
	}
	if !reflect.DeepEqual(ansResult.FilesReceived, []string{"0000abcd.pkt"}) {
		t.Fatalf("answerer FilesReceived = %v, want [0000abcd.pkt]", ansResult.FilesReceived)
	}
	if !reflect.DeepEqual(origResult.FilesSent, []string{"0000abcd.pkt"}) {
		t.Fatalf("originator FilesSent (M_GOT-acknowledged) = %v, want [0000abcd.pkt]", origResult.FilesSent)
	}
}

func TestSessionTransfersFilesInBothDirections(t *testing.T) {
	origContent := []byte("originator -> answerer payload")
	ansContent := []byte("answerer -> originator payload, a bit longer")

	var origReceived, ansReceived bytes.Buffer

	origResult, ansResult, origErr, ansErr := runPair(t,
		Config{
			OurAddresses: []string{"1:234/56.0"},
			OutboundFiles: []OutboundFile{
				{Name: "out1.pkt", Size: int64(len(origContent)), ModTime: time.Now(), Data: bytes.NewReader(origContent)},
			},
			ReceiveFile: func(f InboundFile, r io.Reader) error {
				_, err := io.Copy(&origReceived, r)
				return err
			},
		},
		Config{
			OurAddresses: []string{"1:234/99.0"},
			OutboundFiles: []OutboundFile{
				{Name: "out2.pkt", Size: int64(len(ansContent)), ModTime: time.Now(), Data: bytes.NewReader(ansContent)},
			},
			ReceiveFile: func(f InboundFile, r io.Reader) error {
				_, err := io.Copy(&ansReceived, r)
				return err
			},
		},
	)
	if origErr != nil {
		t.Fatalf("originator error: %v", origErr)
	}
	if ansErr != nil {
		t.Fatalf("answerer error: %v", ansErr)
	}
	if !bytes.Equal(ansReceived.Bytes(), origContent) {
		t.Fatalf("answerer received %q, want %q", ansReceived.Bytes(), origContent)
	}
	if !bytes.Equal(origReceived.Bytes(), ansContent) {
		t.Fatalf("originator received %q, want %q", origReceived.Bytes(), ansContent)
	}
	if !reflect.DeepEqual(origResult.FilesReceived, []string{"out2.pkt"}) {
		t.Fatalf("originator FilesReceived = %v, want [out2.pkt]", origResult.FilesReceived)
	}
	if !reflect.DeepEqual(ansResult.FilesReceived, []string{"out1.pkt"}) {
		t.Fatalf("answerer FilesReceived = %v, want [out1.pkt]", ansResult.FilesReceived)
	}
}

func TestSessionTransfersMultipleFilesFromOneSide(t *testing.T) {
	files := []string{"aaa.pkt", "bbb.pkt", "ccc.pkt"}
	var mu sync.Mutex
	var receivedNames []string

	_, ansResult, origErr, ansErr := runPair(t,
		Config{
			OurAddresses: []string{"1:234/56.0"},
			OutboundFiles: []OutboundFile{
				{Name: files[0], Size: 3, ModTime: time.Now(), Data: strings.NewReader("aaa")},
				{Name: files[1], Size: 3, ModTime: time.Now(), Data: strings.NewReader("bbb")},
				{Name: files[2], Size: 3, ModTime: time.Now(), Data: strings.NewReader("ccc")},
			},
		},
		Config{
			OurAddresses: []string{"1:234/99.0"},
			ReceiveFile: func(f InboundFile, r io.Reader) error {
				buf, err := io.ReadAll(r)
				if err != nil {
					return err
				}
				mu.Lock()
				receivedNames = append(receivedNames, f.Name)
				mu.Unlock()
				_ = buf
				return nil
			},
		},
	)
	if origErr != nil {
		t.Fatalf("originator error: %v", origErr)
	}
	if ansErr != nil {
		t.Fatalf("answerer error: %v", ansErr)
	}
	sort.Strings(receivedNames)
	sort.Strings(ansResult.FilesReceived)
	if !reflect.DeepEqual(receivedNames, files) {
		t.Fatalf("receivedNames = %v, want %v", receivedNames, files)
	}
	if !reflect.DeepEqual(ansResult.FilesReceived, files) {
		t.Fatalf("ansResult.FilesReceived = %v, want %v", ansResult.FilesReceived, files)
	}
}

func TestDialRejectsConfigWithNoAddresses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Dial(ctx, "127.0.0.1:1", Config{})
	if err == nil {
		t.Fatal("expected Dial to reject a Config with no OurAddresses")
	}
}

func TestAnswerRejectsConfigWithNoAddresses(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Answer(ctx, c2, Config{})
	if err == nil {
		t.Fatal("expected Answer to reject a Config with no OurAddresses")
	}
}

func TestSessionContextCancellationAbortsSession(t *testing.T) {
	// The answerer never runs a matching session -- the connection is
	// simply held open -- so the originator's handshake will block on
	// a read that never resolves until ctx cancellation closes it.
	originConn, answerConn := net.Pipe()
	defer answerConn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := runSession(ctx, originConn, Config{OurAddresses: []string{"1:234/56.0"}}, roleOriginator)
	if err == nil {
		t.Fatal("expected an error once the context was cancelled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

// TestOriginatorToleratesMNULAfterMPWD locks in a real interop fix:
// a live uplink was observed sending its "SYS ..." informational line
// interleaved after our M_PWD, before its actual M_OK -- not just
// during the address exchange, which readHandshakeFrames already
// tolerated. The hand-crafted peer below plays exactly that answerer
// behavior over a real TCP loopback (see runPair's doc comment for
// why a real socket, not net.Pipe, is needed for BinkP's handshake).
func TestOriginatorToleratesMNULAfterMPWD(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	peerErrCh := make(chan error, 1)
	go func() {
		peerErrCh <- runQuirkyMNULAfterPWDPeer(ln)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = Dial(ctx, ln.Addr().String(), Config{
		OurAddresses: []string{"1:234/56.0"},
		Password:     "correct horse",
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if err := <-peerErrCh; err != nil {
		t.Fatalf("quirky peer: %v", err)
	}
}

// TestAnswererToleratesMNULBeforeMPWD is runQuirkyMNULAfterPWDPeer's
// mirror image: a hand-crafted caller interleaves an M_NUL line after
// its own M_ADR but before its M_PWD, and a real Answer must not
// mistake that M_NUL for "the caller sent no password" and reject the
// (perfectly legitimate, if delayed) M_PWD that follows.
func TestAnswererToleratesMNULBeforeMPWD(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	ansErrCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			ansErrCh <- err
			return
		}
		defer conn.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err = Answer(ctx, conn, Config{
			OurAddresses: []string{"1:234/99.0"},
			Password:     "correct horse",
		})
		ansErrCh <- err
	}()

	if err := runQuirkyMNULBeforePWDPeer(ln.Addr().String()); err != nil {
		t.Fatalf("quirky peer: %v", err)
	}
	if err := <-ansErrCh; err != nil {
		t.Fatalf("Answer: %v", err)
	}
}

// runQuirkyMNULBeforePWDPeer dials addr and manually plays an
// originator that authenticates in the clear but, unlike this
// package's own Dial, sends an extra M_NUL line after its M_ADR and
// before its M_PWD.
func runQuirkyMNULBeforePWDPeer(addr string) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := writeCommandFrame(conn, MADR, "1:234/56.0"); err != nil {
		return err
	}

	// Read until the answerer's M_ADR (ignoring its M_NUL info lines).
	for {
		isData, payload, err := readFrame(conn)
		if err != nil {
			return err
		}
		if isData || len(payload) == 0 {
			return fmt.Errorf("unexpected frame before M_ADR")
		}
		if Command(payload[0]) == MADR {
			break
		}
	}

	// The quirk under test: an M_NUL line arrives before M_PWD.
	if err := writeCommandFrame(conn, MNUL, "SYS quirky-caller"); err != nil {
		return err
	}
	if err := writeCommandFrame(conn, MPWD, "correct horse"); err != nil {
		return err
	}

	isData, payload, err := readFrame(conn)
	if err != nil {
		return err
	}
	if isData || len(payload) == 0 || Command(payload[0]) != MOK {
		return fmt.Errorf("expected M_OK, got isData=%v payload=%q", isData, payload)
	}

	// Empty transfer phase: send our M_EOB and wait for the answerer's.
	if err := writeCommandFrame(conn, MEOB, ""); err != nil {
		return err
	}
	for {
		isData, payload, err := readFrame(conn)
		if err != nil {
			return err
		}
		if isData || len(payload) == 0 {
			return fmt.Errorf("unexpected frame during transfer phase")
		}
		if Command(payload[0]) == MEOB {
			return nil
		}
	}
}

// runQuirkyMNULAfterPWDPeer accepts one connection and manually plays
// an answerer that authenticates in the clear (no CRAM-MD5 challenge
// offered) but, unlike this package's own Answer, sends an extra
// M_NUL line after the client's M_PWD and before its M_OK.
func runQuirkyMNULAfterPWDPeer(ln net.Listener) error {
	conn, err := ln.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()

	// Read until the client's M_ADR (ignoring its M_NUL info lines),
	// matching readHandshakeFrames' own tolerance.
	for {
		isData, payload, err := readFrame(conn)
		if err != nil {
			return err
		}
		if isData || len(payload) == 0 {
			return fmt.Errorf("unexpected frame before M_ADR")
		}
		if Command(payload[0]) == MADR {
			break
		}
	}

	if err := writeCommandFrame(conn, MNUL, "SYS quirky-peer"); err != nil {
		return err
	}
	if err := writeCommandFrame(conn, MADR, "1:234/99.0"); err != nil {
		return err
	}

	isData, payload, err := readFrame(conn)
	if err != nil {
		return err
	}
	if isData || len(payload) == 0 || Command(payload[0]) != MPWD {
		return fmt.Errorf("expected M_PWD, got isData=%v payload=%q", isData, payload)
	}
	if arg := string(payload[1:]); arg != "correct horse" {
		return fmt.Errorf("M_PWD arg = %q, want the plaintext password", arg)
	}

	// The quirk under test: an M_NUL line arrives before M_OK.
	if err := writeCommandFrame(conn, MNUL, "SYS Clearing Houz"); err != nil {
		return err
	}
	if err := writeCommandFrame(conn, MOK, ""); err != nil {
		return err
	}

	// Empty transfer phase: send our M_EOB and wait for the client's.
	if err := writeCommandFrame(conn, MEOB, ""); err != nil {
		return err
	}
	for {
		isData, payload, err := readFrame(conn)
		if err != nil {
			return err
		}
		if isData || len(payload) == 0 {
			return fmt.Errorf("unexpected frame during transfer phase")
		}
		if Command(payload[0]) == MEOB {
			return nil
		}
	}
}

// TestOriginatorToleratesPeerClosingInsteadOfSendingMEOB locks in a
// real interop fix: a live uplink sent its file, then closed the TCP
// connection immediately afterward instead of sending a formal M_EOB
// first. Since we offered no files of our own (nothing of ours could
// be left unacknowledged), that clean close must be tolerated as an
// implicit "nothing more from me", not surfaced as an error -- see
// receiveLoop's doc comment.
func TestOriginatorToleratesPeerClosingInsteadOfSendingMEOB(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	peerErrCh := make(chan error, 1)
	go func() {
		peerErrCh <- runQuirkyNoMEOBPeer(ln)
	}()

	var received []byte
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := Dial(ctx, ln.Addr().String(), Config{
		OurAddresses: []string{"1:234/56.0"},
		ReceiveFile: func(f InboundFile, r io.Reader) error {
			data, err := io.ReadAll(r)
			received = data
			return err
		},
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if err := <-peerErrCh; err != nil {
		t.Fatalf("quirky peer: %v", err)
	}
	if string(received) != "hello from a peer that skips M_EOB" {
		t.Fatalf("received file content = %q, want the full file", received)
	}
	if len(res.FilesReceived) != 1 || res.FilesReceived[0] != "12345678.pkt" {
		t.Fatalf("FilesReceived = %v, want [12345678.pkt]", res.FilesReceived)
	}
}

// runQuirkyNoMEOBPeer accepts one connection and manually plays an
// answerer that sends one file and then closes the connection
// immediately, without ever sending M_EOB.
func runQuirkyNoMEOBPeer(ln net.Listener) error {
	conn, err := ln.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()

	for {
		isData, payload, err := readFrame(conn)
		if err != nil {
			return err
		}
		if isData || len(payload) == 0 {
			return fmt.Errorf("unexpected frame before M_ADR")
		}
		if Command(payload[0]) == MADR {
			break
		}
	}
	if err := writeCommandFrame(conn, MADR, "1:234/99.0"); err != nil {
		return err
	}

	// The client (having no password configured) sends nothing more
	// before its own M_EOB -- consume it before sending our file, to
	// match a real transfer phase's ordering.
	isData, payload, err := readFrame(conn)
	if err != nil {
		return err
	}
	if isData || len(payload) == 0 || Command(payload[0]) != MEOB {
		return fmt.Errorf("expected the client's M_EOB, got isData=%v payload=%q", isData, payload)
	}

	data := []byte("hello from a peer that skips M_EOB")
	if err := writeCommandFrame(conn, MFILE, fmt.Sprintf("12345678.pkt %d %d 0", len(data), time.Now().Unix())); err != nil {
		return err
	}
	if err := writeDataFrame(conn, data); err != nil {
		return err
	}

	// Wait for the client's M_GOT acknowledging the file, then close
	// without ever sending our own M_EOB -- the quirk under test.
	isData, payload, err = readFrame(conn)
	if err != nil {
		return err
	}
	if isData || len(payload) == 0 || Command(payload[0]) != MGOT {
		return fmt.Errorf("expected M_GOT, got isData=%v payload=%q", isData, payload)
	}
	return nil
}

// TestReceiveOneFileSendsMGOTBeforeReceiveFileProcessingCompletes locks
// in the ack-latency fix: M_GOT must reach the peer as soon as the
// transfer itself is verified complete (all bytes in), not after our
// own ReceiveFile callback (packet parsing, per-message DB writes --
// which can be slow) finishes running. A peer that hangs up right
// after its last data frame instead of waiting for M_EOB (see
// TestOriginatorToleratesPeerClosingInsteadOfSendingMEOB, observed
// live against a real uplink) needs that ack as fast as possible. This
// test deadlocks under the old ordering, where M_GOT was gated on
// ReceiveFile's return: the peer here refuses to unblock our
// deliberately slow ReceiveFile until it has actually seen M_GOT.
func TestReceiveOneFileSendsMGOTBeforeReceiveFileProcessingCompletes(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	unblockProcessing := make(chan struct{})
	peerErrCh := make(chan error, 1)
	go func() {
		peerErrCh <- runSlowConsumerPeer(ln, unblockProcessing)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := Dial(ctx, ln.Addr().String(), Config{
		OurAddresses: []string{"1:234/56.0"},
		ReceiveFile: func(f InboundFile, r io.Reader) error {
			if _, err := io.ReadAll(r); err != nil {
				return err
			}
			<-unblockProcessing
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if err := <-peerErrCh; err != nil {
		t.Fatalf("peer: %v", err)
	}
	if len(res.FilesReceived) != 1 || res.FilesReceived[0] != "slow.pkt" {
		t.Fatalf("FilesReceived = %v, want [slow.pkt]", res.FilesReceived)
	}
}

// runSlowConsumerPeer sends one file, waits to see the client's M_GOT,
// then unblocks the client's deliberately slow ReceiveFile before
// finishing the session normally.
func runSlowConsumerPeer(ln net.Listener, unblockProcessing chan<- struct{}) error {
	conn, err := ln.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()

	for {
		isData, payload, err := readFrame(conn)
		if err != nil {
			return err
		}
		if isData || len(payload) == 0 {
			return fmt.Errorf("unexpected frame before M_ADR")
		}
		if Command(payload[0]) == MADR {
			break
		}
	}
	if err := writeCommandFrame(conn, MADR, "1:234/99.0"); err != nil {
		return err
	}

	isData, payload, err := readFrame(conn)
	if err != nil {
		return err
	}
	if isData || len(payload) == 0 || Command(payload[0]) != MEOB {
		return fmt.Errorf("expected the client's M_EOB, got isData=%v payload=%q", isData, payload)
	}

	data := []byte("slow consumer test payload")
	if err := writeCommandFrame(conn, MFILE, fmt.Sprintf("slow.pkt %d %d 0", len(data), time.Now().Unix())); err != nil {
		return err
	}
	if err := writeDataFrame(conn, data); err != nil {
		return err
	}

	isData, payload, err = readFrame(conn)
	if err != nil {
		return err
	}
	if isData || len(payload) == 0 || Command(payload[0]) != MGOT {
		return fmt.Errorf("expected M_GOT, got isData=%v payload=%q", isData, payload)
	}

	close(unblockProcessing)

	return writeCommandFrame(conn, MEOB, "")
}
