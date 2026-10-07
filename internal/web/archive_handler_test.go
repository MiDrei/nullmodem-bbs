package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/mail"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

func TestListArchiveReturnsCapturedEntries(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c, err := srv.Archive.Begin("21:3/100", "host:24554", "12345678.pkt")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := c.Writer().Write([]byte("raw packet bytes")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := c.Finish("ok", ""); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodGet, "/api/archive", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Entries []archiveEntryDTO `json:"entries"`
		Total   int               `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 || len(resp.Entries) != 1 || resp.Entries[0].Filename != "12345678.pkt" {
		t.Fatalf("response = %+v, want the one captured entry", resp)
	}
}

func TestDownloadArchiveEntryStreamsRawBytes(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c, err := srv.Archive.Begin("21:3/100", "host:24554", "readme.txt")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := c.Writer().Write([]byte("exact bytes")); err != nil {
		t.Fatalf("write: %v", err)
	}
	e, err := c.Finish("ok", "")
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodGet, fmt.Sprintf("/api/archive/%d/download", e.ID), nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "exact bytes" {
		t.Fatalf("downloaded body = %q, want %q", rec.Body.String(), "exact bytes")
	}
}

func TestDeleteArchiveEntryRemovesIt(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c, err := srv.Archive.Begin("21:3/100", "host:24554", "gone.pkt")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	e, err := c.Finish("ok", "")
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodDelete, fmt.Sprintf("/api/archive/%d", e.ID), nil, token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204, body=%s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, h, http.MethodGet, "/api/archive", nil, token)
	var resp struct {
		Total int `json:"total"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Total != 0 {
		t.Fatalf("total after delete = %d, want 0", resp.Total)
	}
}

func TestInspectArchiveEntryReturnsPacketSummary(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}

	var buf bytes.Buffer
	w, err := mail.NewWriter(&buf, mail.PacketHeader{
		OrigAddr: mail.Address{Zone: 21, Net: 3, Node: 100},
		DestAddr: mail.Address{Zone: 21, Net: 3, Node: 194},
		Created:  time.Now(),
	})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.WriteMessage(mail.Message{ToName: "Bob", FromName: "Alice", Subject: "Hi", Body: "hello"}); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	c, err := srv.Archive.Begin("21:3/100", "host:24554", "12345678.pkt")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := c.Writer().Write(buf.Bytes()); err != nil {
		t.Fatalf("write: %v", err)
	}
	e, err := c.Finish("ok", "")
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodGet, fmt.Sprintf("/api/archive/%d/inspect", e.ID), nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Kind    string               `json:"kind"`
		Packets []inspectedPacketDTO `json:"packets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Kind != "packet" || len(resp.Packets) != 1 || len(resp.Packets[0].Messages) != 1 {
		t.Fatalf("response = %+v, want one packet with one message", resp)
	}
	if resp.Packets[0].Messages[0].Subject != "Hi" || resp.Packets[0].Messages[0].FromName != "Alice" {
		t.Fatalf("message summary = %+v", resp.Packets[0].Messages[0])
	}
}

func TestRetossArchiveEntriesReplaysAPacket(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := users.Register("bob", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}

	var buf bytes.Buffer
	w, err := mail.NewWriter(&buf, mail.PacketHeader{
		OrigAddr: mail.Address{Zone: 21, Net: 3, Node: 100},
		DestAddr: mail.Address{Zone: 21, Net: 3, Node: 194},
		Created:  time.Now(),
	})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.WriteMessage(mail.Message{ToName: "Bob", FromName: "Alice", Subject: "Hi", Body: "hello"}); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	c, err := srv.Archive.Begin("21:3/100", "host:24554", "12345678.pkt")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := c.Writer().Write(buf.Bytes()); err != nil {
		t.Fatalf("write: %v", err)
	}
	e, err := c.Finish("ok", "")
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodPost, "/api/archive/retoss", map[string]any{"ids": []int64{e.ID}}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Received int `json:"received"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Received != 1 {
		t.Fatalf("received = %d, want 1", resp.Received)
	}
}

func TestRetossArchiveEntriesRejectsEmptyIDs(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodPost, "/api/archive/retoss", map[string]any{"ids": []int64{}}, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}
