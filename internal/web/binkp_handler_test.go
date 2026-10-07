package web

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/binkp"
	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

// startTestAnswerer runs a single BinkP answering session on a local
// loopback listener and returns its address, so handler tests can
// point handleTestBinkpConnection at a real (if minimal) peer instead
// of a live uplink.
func startTestAnswerer(t *testing.T, password string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		binkp.Answer(ctx, conn, binkp.Config{
			OurAddresses: []string{"21:3/194"},
			Password:     password,
		})
	}()
	return ln.Addr().String()
}

func TestTestBinkpConnectionRejectsMissingHost(t *testing.T) {
	srv, users, configPath := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	c.BBS.FTNAddresses = []string{"21:3/194.1"}
	if err := config.Save(configPath, c); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodPost, "/api/binkp/test-connection", binkpUplinkDTO{}, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestTestBinkpConnectionRequiresOwnFTNAddress(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodPost, "/api/binkp/test-connection", binkpUplinkDTO{
		Host: "127.0.0.1:1", // unreachable, but we should fail validation before dialing
	}, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (missing own FTN address), body=%s", rec.Code, rec.Body.String())
	}
}

func TestTestBinkpConnectionSucceedsAgainstLocalAnswerer(t *testing.T) {
	srv, users, configPath := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	c.BBS.FTNAddresses = []string{"21:3/194.1"}
	if err := config.Save(configPath, c); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	addr := startTestAnswerer(t, "correct horse")

	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodPost, "/api/binkp/test-connection", binkpUplinkDTO{
		Host:     addr,
		Password: "correct horse",
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		RemoteAddresses []string `json:"remote_addresses"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.RemoteAddresses) != 1 || resp.RemoteAddresses[0] != "21:3/194" {
		t.Fatalf("remote_addresses = %v, want [21:3/194]", resp.RemoteAddresses)
	}
}

func TestTestBinkpConnectionReportsAuthFailure(t *testing.T) {
	srv, users, configPath := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	c.BBS.FTNAddresses = []string{"21:3/194.1"}
	if err := config.Save(configPath, c); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	addr := startTestAnswerer(t, "correct horse")

	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodPost, "/api/binkp/test-connection", binkpUplinkDTO{
		Host:     addr,
		Password: "wrong password",
	}, token)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502, body=%s", rec.Code, rec.Body.String())
	}
}

func TestSendNowBinkpRequiresOwnFTNAddress(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodPost, "/api/binkp/send-now", binkpUplinkDTO{
		Host: "127.0.0.1:1",
	}, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (missing own FTN address), body=%s", rec.Code, rec.Body.String())
	}
}

func TestSendNowBinkpSendsQueuedNetmailAndReportsCounts(t *testing.T) {
	srv, users, configPath := newTestServer(t)
	root, err := users.Register("root", "supersecret", user.SLSysop)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	c, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	c.BBS.FTNAddresses = []string{"21:3/194.1"}
	if err := config.Save(configPath, c); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	if _, err := srv.Netmail.Send(root.ID, "21:3/194.1", 0, "Mike Dreier", "21:3/194", "Hi", "body", false); err != nil {
		t.Fatalf("Netmail.Send: %v", err)
	}

	var received int64
	addr := startTestAnswererCountingFiles(t, "correct horse", &received)

	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodPost, "/api/binkp/send-now", binkpUplinkDTO{
		Address:  "21:3/194",
		Host:     addr,
		Password: "correct horse",
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Sent     int `json:"sent"`
		Received int `json:"received"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Sent != 1 {
		t.Fatalf("sent = %d, want 1", resp.Sent)
	}

	pending, err := srv.Netmail.PendingOutbound()
	if err != nil {
		t.Fatalf("PendingOutbound: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("PendingOutbound after send-now = %+v, want empty", pending)
	}
}

// startTestAnswererCountingFiles is startTestAnswerer plus a
// ReceiveFile that drains (and counts) whatever the caller sends, so
// handleSendNowBinkp's outbound packet doesn't stall the session.
func startTestAnswererCountingFiles(t *testing.T, password string, filesReceived *int64) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		binkp.Answer(ctx, conn, binkp.Config{
			OurAddresses: []string{"21:3/194"},
			Password:     password,
			ReceiveFile: func(f binkp.InboundFile, r io.Reader) error {
				*filesReceived++
				_, err := io.Copy(io.Discard, r)
				return err
			},
		})
	}()
	return ln.Addr().String()
}

// startTestAnswererCapturingRemoteAddresses is startTestAnswerer plus
// reporting the addresses the caller actually presented via M_ADR
// (binkp.Result.RemoteAddresses on the answerer's own side) -- lets a
// test assert exactly which addresses a handler presented, not just
// that the session succeeded.
func startTestAnswererCapturingRemoteAddresses(t *testing.T, password string) (addr string, remoteAddresses <-chan []string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	ch := make(chan []string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			ch <- nil
			return
		}
		defer conn.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		res, err := binkp.Answer(ctx, conn, binkp.Config{
			OurAddresses: []string{"21:3/194"},
			Password:     password,
			ReceiveFile: func(f binkp.InboundFile, r io.Reader) error {
				_, err := io.Copy(io.Discard, r)
				return err
			},
		})
		if err != nil || res == nil {
			ch <- nil
			return
		}
		ch <- res.RemoteAddresses
	}()
	return ln.Addr().String(), ch
}

// TestSendNowBinkpPresentsOnlyRestrictedAKAAddresses is a regression
// test: handleSendNowBinkp used to build its config.BinkpUplink
// straight from the request body without copying AKAAddresses, so a
// restricted uplink's "Send Now" silently presented every configured
// address instead of just the restricted subset (confirmed live).
func TestSendNowBinkpPresentsOnlyRestrictedAKAAddresses(t *testing.T) {
	srv, users, configPath := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	c.BBS.FTNAddresses = []string{"21:3/194.1", "954:700/14"}
	if err := config.Save(configPath, c); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	addr, remoteAddresses := startTestAnswererCapturingRemoteAddresses(t, "correct horse")

	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodPost, "/api/binkp/send-now", binkpUplinkDTO{
		Address:      "21:3/194",
		Host:         addr,
		Password:     "correct horse",
		AKAAddresses: []string{"21:3/194.1"},
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	got := <-remoteAddresses
	if len(got) != 1 || got[0] != "21:3/194.1" {
		t.Fatalf("presented addresses = %v, want exactly [21:3/194.1] (the restricted subset, not every configured address)", got)
	}
}

// TestTestBinkpConnectionPresentsOnlyRestrictedAKAAddresses is
// handleTestBinkpConnection's counterpart to the regression test
// above -- it never even looked at AKAAddresses.
func TestTestBinkpConnectionPresentsOnlyRestrictedAKAAddresses(t *testing.T) {
	srv, users, configPath := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	c.BBS.FTNAddresses = []string{"21:3/194.1", "954:700/14"}
	if err := config.Save(configPath, c); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	addr, remoteAddresses := startTestAnswererCapturingRemoteAddresses(t, "correct horse")

	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodPost, "/api/binkp/test-connection", binkpUplinkDTO{
		Host:         addr,
		Password:     "correct horse",
		AKAAddresses: []string{"21:3/194.1"},
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	got := <-remoteAddresses
	if len(got) != 1 || got[0] != "21:3/194.1" {
		t.Fatalf("presented addresses = %v, want exactly [21:3/194.1] (the restricted subset, not every configured address)", got)
	}
}
