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

	"git.maik.ch/swissmaik/nullmodem/internal/version"
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

// countingConn is a minimal net.Conn (only Write is ever exercised)
// that records every separate Write call it receives -- for verifying
// sendInfoAndAddress batches its frames into one underlying Write
// rather than one per frame.
type countingConn struct {
	net.Conn
	buf        bytes.Buffer
	writeCount int
}

func (c *countingConn) Write(b []byte) (int, error) {
	c.writeCount++
	return c.buf.Write(b)
}

// TestSendInfoAndAddressUsesASingleWrite locks in a real production
// fix: a real peer's own log (Mystic BBS, SysopNet) showed it
// sometimes receiving only our very first frame (VER) and nothing
// else -- "Client did not send address" -- right before dropping the
// connection, while an otherwise-identical dial moments later
// succeeded and logged every line. Four separate Writes for one
// logical "here's who I am" burst apparently sometimes arrived as
// more TCP segments than that peer's own read loop kept reading
// across. sendInfoAndAddress must emit VER/SYS/ZYZ/LOC/M_ADR as a
// single underlying Write, not one per frame, regardless of whose
// behavior is technically spec-correct.
func TestSendInfoAndAddressUsesASingleWrite(t *testing.T) {
	conn := &countingConn{}
	s := &session{conn: conn, cfg: Config{SysName: "Test BBS", Sysop: "Ops", Location: "Zurich"}}

	if err := s.sendInfoAndAddress([]string{"1:234/56.0", "21:1/100@fsxnet"}); err != nil {
		t.Fatalf("sendInfoAndAddress: %v", err)
	}
	if conn.writeCount != 1 {
		t.Fatalf("Write was called %d times, want exactly 1 (a single underlying Write for the whole info+address burst)", conn.writeCount)
	}

	r := bytes.NewReader(conn.buf.Bytes())
	var got []string
	for {
		isData, payload, err := readFrame(r)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("readFrame: %v", err)
		}
		if isData {
			t.Fatal("unexpected data frame")
		}
		got = append(got, fmt.Sprintf("%s %s", Command(payload[0]), string(payload[1:])))
	}
	want := []string{
		"M_NUL VER NullModem-BinkP/" + version.Short() + " binkp/1.1",
		"M_NUL SYS Test BBS",
		"M_NUL ZYZ Ops",
		"M_NUL LOC Zurich",
		"M_ADR 1:234/56.0 21:1/100@fsxnet",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("frames = %v, want %v", got, want)
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

// TestSessionPasswordForAddressesPicksPasswordByCaller locks in
// multi-uplink inbound support: an answerer with PasswordForAddresses
// set (instead of a single static Password) picks the right password
// to check based on the caller's M_ADR, letting one listener serve
// several known callers each with their own password.
func TestSessionPasswordForAddressesPicksPasswordByCaller(t *testing.T) {
	origResult, ansResult, origErr, ansErr := runPair(t,
		Config{OurAddresses: []string{"1:234/56.0"}, Password: "carols-password"},
		Config{
			OurAddresses: []string{"1:234/99.0"},
			PasswordForAddresses: func(peerAddrs []string) (string, bool) {
				for _, a := range peerAddrs {
					if a == "1:234/56.0" {
						return "carols-password", true
					}
				}
				return "", false
			},
		},
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

// TestSessionPasswordForAddressesRejectsUnrecognizedCaller locks in
// the reject path: a caller PasswordForAddresses doesn't recognize is
// refused outright, before even exchanging M_PWD.
func TestSessionPasswordForAddressesRejectsUnrecognizedCaller(t *testing.T) {
	_, _, origErr, ansErr := runPair(t,
		Config{OurAddresses: []string{"1:234/77.0"}, Password: "whatever"},
		Config{
			OurAddresses: []string{"1:234/99.0"},
			PasswordForAddresses: func(peerAddrs []string) (string, bool) {
				return "", false
			},
		},
	)
	if origErr == nil {
		t.Fatal("expected the originator to see the session fail")
	}
	if ansErr == nil {
		t.Fatal("expected the answerer to reject the unrecognized caller")
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

// TestMultiBatchSendsFilesQueuedDuringTheSession locks in FSP-1024
// section 4.2 ("Re-initialise session after EOB"): against a peer
// that also advertises binkp/1.1 (this package's own default -- see
// sendInfoAndAddress), a file RescanOutboundFiles only surfaces after
// the first batch already ended must still go out in the very same
// session, in a second batch, rather than waiting for the next poll.
func TestMultiBatchSendsFilesQueuedDuringTheSession(t *testing.T) {
	var rescanCalls int
	var mu sync.Mutex
	var receivedNames []string

	origResult, ansResult, origErr, ansErr := runPair(t,
		Config{
			OurAddresses: []string{"1:234/56.0"},
			OutboundFiles: []OutboundFile{
				{Name: "first.pkt", Size: 3, ModTime: time.Now(), Data: strings.NewReader("one")},
			},
			RescanOutboundFiles: func() []OutboundFile {
				rescanCalls++
				if rescanCalls == 1 {
					return []OutboundFile{
						{Name: "second.pkt", Size: 3, ModTime: time.Now(), Data: strings.NewReader("two")},
					}
				}
				return nil
			},
		},
		Config{
			OurAddresses: []string{"1:234/99.0"},
			ReceiveFile: func(f InboundFile, r io.Reader) error {
				if _, err := io.ReadAll(r); err != nil {
					return err
				}
				mu.Lock()
				receivedNames = append(receivedNames, f.Name)
				mu.Unlock()
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
	if rescanCalls != 2 {
		t.Fatalf("RescanOutboundFiles was called %d times, want exactly 2 (once after each of the two non-empty batches)", rescanCalls)
	}

	want := []string{"first.pkt", "second.pkt"}
	sort.Strings(receivedNames)
	if !reflect.DeepEqual(receivedNames, want) {
		t.Fatalf("receivedNames = %v, want %v", receivedNames, want)
	}
	sort.Strings(ansResult.FilesReceived)
	if !reflect.DeepEqual(ansResult.FilesReceived, want) {
		t.Fatalf("ansResult.FilesReceived = %v, want %v", ansResult.FilesReceived, want)
	}
	sentNames := append([]string(nil), origResult.FilesSent...)
	sort.Strings(sentNames)
	if !reflect.DeepEqual(sentNames, want) {
		t.Fatalf("origResult.FilesSent = %v, want %v", sentNames, want)
	}
}

// TestFallsBackToSingleBatchAgainstABinkp10Peer locks in FSP-1024's
// own fallback requirement (section 3: "if a connection is made with
// a binkp/1.0 mailer the implementation must fallback to the
// binkp/1.0 protocol"): a peer that never claims binkp/1.1 must get
// exactly this package's original one-round behavior -- the session
// ends right after the first M_EOB exchange, and RescanOutboundFiles
// must never even be called.
func TestFallsBackToSingleBatchAgainstABinkp10Peer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	peerErrCh := make(chan error, 1)
	go func() { peerErrCh <- runBinkp10OnlyPeer(ln) }()

	var rescanCalls int
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := Dial(ctx, ln.Addr().String(), Config{
		OurAddresses: []string{"1:234/56.0"},
		OutboundFiles: []OutboundFile{
			{Name: "first.pkt", Size: 3, ModTime: time.Now(), Data: strings.NewReader("one")},
		},
		RescanOutboundFiles: func() []OutboundFile {
			rescanCalls++
			return []OutboundFile{{Name: "should-not-send.pkt", Size: 1, ModTime: time.Now(), Data: strings.NewReader("x")}}
		},
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if err := <-peerErrCh; err != nil {
		t.Fatalf("binkp/1.0 peer: %v", err)
	}
	if rescanCalls != 0 {
		t.Fatalf("RescanOutboundFiles was called %d times, want 0 against a binkp/1.0 peer", rescanCalls)
	}
	if len(result.FilesSent) != 1 || result.FilesSent[0] != "first.pkt" {
		t.Fatalf("FilesSent = %v, want exactly [first.pkt]", result.FilesSent)
	}
}

// runBinkp10OnlyPeer plays a plain binkp/1.0 answerer -- its VER line
// ends in "binkp/1.0", never "binkp/1.1" -- that reads the client's
// one file and M_EOB, acknowledges it, sends its own empty M_EOB, and
// stops there: exactly what a real binkp/1.0-only mailer does and
// nothing more, in particular never offering or expecting a second
// batch.
func runBinkp10OnlyPeer(ln net.Listener) error {
	conn, err := ln.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := writeCommandFrame(conn, MNUL, "VER OldMailer/1.0 binkp/1.0"); err != nil {
		return err
	}
	if err := writeCommandFrame(conn, MADR, "1:234/99.0"); err != nil {
		return err
	}

	sawFile := false
	received := 0
	for {
		isData, payload, err := readFrame(conn)
		if err != nil {
			return err
		}
		if isData {
			received += len(payload)
			continue
		}
		if len(payload) == 0 {
			return fmt.Errorf("empty command frame")
		}
		cmd := Command(payload[0])
		if cmd == MFILE {
			sawFile = true
		}
		if cmd == MEOB {
			break
		}
	}
	if !sawFile || received != 3 {
		return fmt.Errorf("expected exactly one 3-byte file before M_EOB, sawFile=%v received=%d", sawFile, received)
	}
	if err := writeCommandFrame(conn, MGOT, "first.pkt 3 0"); err != nil {
		return err
	}
	return writeCommandFrame(conn, MEOB, "")
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

// TestDialWaitsForPeerToCloseFirstBeforeItsOwnClose locks in the
// close-grace-period fix (see closeGracePeriod's doc comment): Dial
// must not race to close its end the instant the session is done --
// it should give a peer that lingers a beat to close on its own
// first. A peer that deliberately delays its close briefly after a
// clean M_EOB exchange must still result in a successful Dial, with
// the call actually blocking until that peer closes (not returning
// before the peer has had its chance).
func TestDialWaitsForPeerToCloseFirstBeforeItsOwnClose(t *testing.T) {
	orig := closeGracePeriod
	closeGracePeriod = 2 * time.Second
	defer func() { closeGracePeriod = orig }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	const peerDelay = 300 * time.Millisecond
	peerErrCh := make(chan error, 1)
	go func() {
		peerErrCh <- runDelayedClosePeer(ln, peerDelay)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	res, err := Dial(ctx, ln.Addr().String(), Config{
		OurAddresses: []string{"1:234/56.0"},
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if err := <-peerErrCh; err != nil {
		t.Fatalf("delayed-close peer: %v", err)
	}
	if elapsed < peerDelay {
		t.Fatalf("Dial returned after %v, want it to have waited at least the peer's %v close delay", elapsed, peerDelay)
	}
	if elapsed >= closeGracePeriod {
		t.Fatalf("Dial took %v, want well under the %v grace period since the peer did close on its own", elapsed, closeGracePeriod)
	}
	if res == nil {
		t.Fatal("Dial returned a nil Result")
	}
}

// runDelayedClosePeer accepts one connection, completes a normal
// empty-transfer handshake (mirroring TestSessionHandshakeAndEmpty
// TransferNoPassword), waits delay, then closes.
func runDelayedClosePeer(ln net.Listener, delay time.Duration) error {
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
			break
		}
	}

	time.Sleep(delay)
	return nil
}

// TestDialSelfClosesAfterGracePeriodIfPeerNeverDoes locks in the
// upper bound on closeGracePeriod: a peer that never closes at all
// must not hang Dial forever -- it gives up and lets its own Close
// happen once the grace period elapses.
func TestDialSelfClosesAfterGracePeriodIfPeerNeverDoes(t *testing.T) {
	orig := closeGracePeriod
	closeGracePeriod = 200 * time.Millisecond
	defer func() { closeGracePeriod = orig }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	peerErrCh := make(chan error, 1)
	go func() {
		peerErrCh <- runNeverClosingPeer(ln)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	res, err := Dial(ctx, ln.Addr().String(), Config{
		OurAddresses: []string{"1:234/56.0"},
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if elapsed < closeGracePeriod {
		t.Fatalf("Dial returned after %v, want it to have waited out the %v grace period", elapsed, closeGracePeriod)
	}
	if elapsed > closeGracePeriod+2*time.Second {
		t.Fatalf("Dial took %v, way past the %v grace period", elapsed, closeGracePeriod)
	}
	if res == nil {
		t.Fatal("Dial returned a nil Result")
	}
	_ = <-peerErrCh // the peer never returns on its own; conn closing (Dial's defer) unblocks its read
}

// runNeverClosingPeer accepts one connection, completes a normal
// empty-transfer handshake, then holds the connection open (reading,
// which blocks until Dial's side eventually closes it) instead of
// ever closing its own end.
func runNeverClosingPeer(ln net.Listener) error {
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
			break
		}
	}

	// Hold the connection open past the test's shrunk grace period --
	// this read only returns once Dial's own defer conn.Close() fires.
	_, _, _ = readFrame(conn)
	return nil
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

// TestReceiveOneFileAcknowledgesWithNameSizeAndTimestamp locks in a
// real interop fix: M_GOT must echo back all three of M_FILE's fields
// -- name, size, AND timestamp -- not just name and size. Confirmed
// against binkd's own source (protocol.c's GOT()/tfile_cmp(), which
// requires exactly 3 arguments and matches all three against the
// outbound file record) and against binkterm-php's own M_GOT
// construction (this project's reference implementation, see
// CLAUDE.md). A 2-field M_GOT fails binkd's argument parsing outright
// -- a very plausible reason a real uplink's mail never registered as
// delivered no matter how quickly or cleanly this side otherwise
// behaved.
func TestReceiveOneFileAcknowledgesWithNameSizeAndTimestamp(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	const fileTimestamp = 1700000000 // arbitrary fixed Unix time

	gotArgCh := make(chan string, 1)
	peerErrCh := make(chan error, 1)
	go func() {
		peerErrCh <- runTimestampCheckingPeer(ln, fileTimestamp, gotArgCh)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = Dial(ctx, ln.Addr().String(), Config{
		OurAddresses: []string{"1:234/56.0"},
		ReceiveFile: func(f InboundFile, r io.Reader) error {
			_, err := io.ReadAll(r)
			return err
		},
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if err := <-peerErrCh; err != nil {
		t.Fatalf("peer: %v", err)
	}

	gotArg := <-gotArgCh
	want := fmt.Sprintf("timestamped.pkt 20 %d", fileTimestamp)
	if gotArg != want {
		t.Fatalf("M_GOT argument = %q, want %q (name, size, timestamp)", gotArg, want)
	}
}

// runTimestampCheckingPeer sends one file stamped with timestamp,
// captures the client's M_GOT argument string verbatim onto gotArgCh,
// then finishes the session normally.
func runTimestampCheckingPeer(ln net.Listener, timestamp int64, gotArgCh chan<- string) error {
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

	data := []byte("12345678901234567890")[:20]
	if err := writeCommandFrame(conn, MFILE, fmt.Sprintf("timestamped.pkt %d %d 0", len(data), timestamp)); err != nil {
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
	gotArgCh <- string(payload[1:])

	// The client already sent its own M_EOB during the handshake
	// phase (it offers no outbound files); send ours now and close --
	// nothing more is coming from either side.
	return writeCommandFrame(conn, MEOB, "")
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

// fakeRecorder implements SessionRecorder, capturing "direction: line"
// entries in order for a test to inspect.
type fakeRecorder struct {
	mu    sync.Mutex
	lines []string
}

func (r *fakeRecorder) RecordFrame(direction, line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, direction+": "+line)
}

// TestSessionRecorderRedactsPasswordAndSummarizesDataFrames locks in
// SessionRecorder's two safety properties: M_PWD's argument never
// reaches the recorder (redacted to "***" on both the sending and
// receiving side, regardless of whether it's plaintext or a CRAM-MD5
// digest), and a data frame is summarized by its byte count only,
// never its actual content.
func TestSessionRecorderRedactsPasswordAndSummarizesDataFrames(t *testing.T) {
	origRec := &fakeRecorder{}
	ansRec := &fakeRecorder{}

	origResult, ansResult, origErr, ansErr := runPair(t,
		Config{
			OurAddresses: []string{"1:234/56.0"},
			Password:     "super-secret",
			Recorder:     origRec,
			OutboundFiles: []OutboundFile{
				{Name: "test.pkt", Size: 5, ModTime: time.Now(), Data: bytes.NewReader([]byte("hello"))},
			},
		},
		Config{
			OurAddresses: []string{"21:1/100"},
			Password:     "super-secret",
			Recorder:     ansRec,
		},
	)
	if origErr != nil || ansErr != nil {
		t.Fatalf("runPair: origErr=%v ansErr=%v", origErr, ansErr)
	}
	if len(origResult.FilesSent) != 1 || len(ansResult.FilesReceived) != 1 {
		t.Fatalf("expected the one file to transfer: origResult=%+v ansResult=%+v", origResult, ansResult)
	}

	for _, rec := range []*fakeRecorder{origRec, ansRec} {
		for _, line := range rec.lines {
			if strings.Contains(line, "super-secret") {
				t.Fatalf("recorded line leaked the password: %q (all lines: %v)", line, rec.lines)
			}
		}
	}

	wantLine := func(lines []string, want string) bool {
		for _, l := range lines {
			if l == want {
				return true
			}
		}
		return false
	}
	if !wantLine(origRec.lines, "send: M_PWD ***") {
		t.Fatalf("originator lines = %v, want a redacted \"send: M_PWD ***\"", origRec.lines)
	}
	if !wantLine(ansRec.lines, "recv: M_PWD ***") {
		t.Fatalf("answerer lines = %v, want a redacted \"recv: M_PWD ***\"", ansRec.lines)
	}
	if !wantLine(origRec.lines, "send: DATA 5 bytes") {
		t.Fatalf("originator lines = %v, want \"send: DATA 5 bytes\" (content summarized, not embedded)", origRec.lines)
	}
	if !wantLine(ansRec.lines, "recv: DATA 5 bytes") {
		t.Fatalf("answerer lines = %v, want \"recv: DATA 5 bytes\"", ansRec.lines)
	}
}

// TestMGOTWithEmptyArgumentIsRejectedNotPanicked locks in a real fix:
// receiveBatch used to index strings.Fields(arg)[0] unconditionally
// for M_GOT, so a peer sending a bare M_GOT with no argument at all
// (trivially producible, whether a buggy implementation or a
// deliberate probe) panicked the whole process instead of failing
// this one session -- confirmed against binkd's own GOT() (protocol.c),
// which cleanly rejects anything short of 3 parseable arguments rather
// than indexing past what it verified is present. A real answer must
// now surface this as an ordinary error.
func TestMGOTWithEmptyArgumentIsRejectedNotPanicked(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	go func() { _ = runBareMGOTPeer(ln) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = Dial(ctx, ln.Addr().String(), Config{
		OurAddresses: []string{"1:234/56.0"},
		OutboundFiles: []OutboundFile{
			{Name: "first.pkt", Size: 3, ModTime: time.Now(), Data: strings.NewReader("one")},
		},
	})
	if err == nil {
		t.Fatal("expected Dial to fail on a bare M_GOT, not succeed")
	}
}

// runBareMGOTPeer accepts one connection, reads the client's one file,
// and replies with a completely empty M_GOT instead of a real
// acknowledgment.
func runBareMGOTPeer(ln net.Listener) error {
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

	// Drain the client's M_FILE, its data, and its M_EOB.
	for {
		isData, payload, err := readFrame(conn)
		if err != nil {
			return err
		}
		if !isData && len(payload) > 0 && Command(payload[0]) == MEOB {
			break
		}
	}

	return writeCommandFrame(conn, MGOT, "")
}

// TestMGOTForAFileNeverSentIsIgnored locks in pendingSent's own cross-
// check (see its doc comment): a M_GOT naming a file this side never
// actually sent -- garbled, stray, or a plain lie -- must not be
// credited as an acknowledgment of anything, even though a real
// M_GOT for the file actually sent must still be accepted normally
// right alongside it.
func TestMGOTForAFileNeverSentIsIgnored(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	peerErrCh := make(chan error, 1)
	go func() { peerErrCh <- runGarbageThenRealGOTPeer(ln) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := Dial(ctx, ln.Addr().String(), Config{
		OurAddresses: []string{"1:234/56.0"},
		OutboundFiles: []OutboundFile{
			{Name: "real.pkt", Size: 3, ModTime: time.Now(), Data: strings.NewReader("one")},
		},
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if err := <-peerErrCh; err != nil {
		t.Fatalf("peer: %v", err)
	}
	want := []string{"real.pkt"}
	if !reflect.DeepEqual(result.FilesSent, want) {
		t.Fatalf("FilesSent = %v, want %v (the garbage M_GOT must not appear)", result.FilesSent, want)
	}
}

// runGarbageThenRealGOTPeer reads the client's one file, then sends a
// M_GOT for a name that was never offered before sending the real
// acknowledgment for the file actually received.
func runGarbageThenRealGOTPeer(ln net.Listener) error {
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

	for {
		isData, payload, err := readFrame(conn)
		if err != nil {
			return err
		}
		if !isData && len(payload) > 0 && Command(payload[0]) == MEOB {
			break
		}
	}

	if err := writeCommandFrame(conn, MGOT, "nonexistent.pkt 999 0"); err != nil {
		return err
	}
	if err := writeCommandFrame(conn, MGOT, "real.pkt 3 0"); err != nil {
		return err
	}
	return writeCommandFrame(conn, MEOB, "")
}

// TestMidTransferMEOBAbortsOnlyThatFileNotTheSession locks in binkd's
// own tolerance (confirmed against its source, protocol.c's EOB()):
// a peer that sends M_EOB before finishing a file it announced --
// binkd's own comment calls this "due to remote bug" -- must not
// abort the whole session. The partial file is discarded and the
// session ends normally instead.
func TestMidTransferMEOBAbortsOnlyThatFileNotTheSession(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	peerErrCh := make(chan error, 1)
	go func() { peerErrCh <- runEarlyMEOBPeer(ln) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := Dial(ctx, ln.Addr().String(), Config{
		OurAddresses: []string{"1:234/56.0"},
		ReceiveFile: func(f InboundFile, r io.Reader) error {
			_, err := io.ReadAll(r)
			return err
		},
	})
	if err != nil {
		t.Fatalf("Dial: %v (expected the session to tolerate the early M_EOB and succeed)", err)
	}
	if len(result.FilesReceived) != 0 {
		t.Fatalf("FilesReceived = %v, want none (the interrupted file must not count as received)", result.FilesReceived)
	}
}

// runEarlyMEOBPeer announces a 20-byte file, sends only 5 bytes of
// it, then sends M_EOB instead of the remaining data -- exactly the
// "remote bug" binkd's own EOB() tolerates.
func runEarlyMEOBPeer(ln net.Listener) error {
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

	if err := writeCommandFrame(conn, MFILE, "interrupted.pkt 20 1700000000 0"); err != nil {
		return err
	}
	if err := writeDataFrame(conn, []byte("12345")); err != nil {
		return err
	}
	// The "remote bug": M_EOB instead of the remaining 15 bytes.
	return writeCommandFrame(conn, MEOB, "")
}

// TestFileNameWithSpaceSurvivesTheWireRoundTrip locks in
// quoteFileName/dequoteFileName (see their own doc comments, and
// binkd's tools.c strquote/strdequote which they mirror): a filename
// containing a space -- e.g. a file forwarded via TIC under its
// original, user-chosen name -- must arrive at the other end with the
// exact same name, not truncated or corrupted by M_FILE's whitespace-
// delimited argument parsing.
func TestFileNameWithSpaceSurvivesTheWireRoundTrip(t *testing.T) {
	const name = "my file with spaces.zip"
	var receivedName string

	_, _, origErr, ansErr := runPair(t,
		Config{
			OurAddresses: []string{"1:234/56.0"},
			OutboundFiles: []OutboundFile{
				{Name: name, Size: 3, ModTime: time.Now(), Data: strings.NewReader("abc")},
			},
		},
		Config{
			OurAddresses: []string{"1:234/99.0"},
			ReceiveFile: func(f InboundFile, r io.Reader) error {
				receivedName = f.Name
				_, err := io.ReadAll(r)
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
	if receivedName != name {
		t.Fatalf("received file name = %q, want %q", receivedName, name)
	}
}
