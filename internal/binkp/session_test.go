package binkp

import (
	"bytes"
	"context"
	"errors"
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
