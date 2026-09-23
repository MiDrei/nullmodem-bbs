package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

func TestListUnresolvedNetmailReturnsUndeliverableMail(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// A mistyped-recipient message: no local user, no remote address
	// either -- exactly what UnresolvedInbox surfaces.
	if _, err := srv.Netmail.SendSystem("Areafix", "9999:1/1", "nobody-by-this-name", "", "Re: subscribe", "+TEST: added\r", true); err != nil {
		t.Fatalf("SendSystem: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodGet, "/api/netmail/unresolved", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var msgs []unresolvedNetmailSummaryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &msgs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(msgs) != 1 || msgs[0].ToName != "nobody-by-this-name" {
		t.Fatalf("msgs = %+v, want the one undeliverable message", msgs)
	}
}

func TestUnresolvedNetmailExcludesResolvableMail(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	bob, err := users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Resolves to a real local user -- must never show up here.
	if _, err := srv.Netmail.Send(alice.ID, "", bob.ID, "bob", "", "Hi", "body", false); err != nil {
		t.Fatalf("Send (local): %v", err)
	}
	// Routed to a remote FTN address -- also not "stuck", so also
	// excluded.
	if _, err := srv.Netmail.Send(alice.ID, "", 0, "21:3/100", "21:3/100", "Hi remote", "body", false); err != nil {
		t.Fatalf("Send (remote): %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodGet, "/api/netmail/unresolved", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var msgs []unresolvedNetmailSummaryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &msgs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("msgs = %+v, want empty -- neither message is actually stuck", msgs)
	}
}

func TestGetAndDeleteUnresolvedNetmail(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	queued, err := srv.Netmail.SendSystem("Areafix", "9999:1/1", "nobody-by-this-name", "", "Re: subscribe", "+TEST: added\r", true)
	if err != nil {
		t.Fatalf("SendSystem: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	getPath := fmt.Sprintf("/api/netmail/unresolved/%d", queued.ID)
	rec := doJSON(t, h, http.MethodGet, getPath, nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got unresolvedNetmailDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Body != "+TEST: added\r" {
		t.Fatalf("Body = %q, want the full body", got.Body)
	}

	rec = doJSON(t, h, http.MethodDelete, getPath, nil, token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204, body=%s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, h, http.MethodGet, getPath, nil, token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET after delete status = %d, want 404", rec.Code)
	}
}

func TestBatchDeleteUnresolvedNetmailDeletesSelectedMessages(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	a, err := srv.Netmail.SendSystem("Areafix", "9999:1/1", "nobody-1", "", "Re: subscribe", "body", true)
	if err != nil {
		t.Fatalf("SendSystem: %v", err)
	}
	b, err := srv.Netmail.SendSystem("Areafix", "9999:1/1", "nobody-2", "", "Re: subscribe", "body", true)
	if err != nil {
		t.Fatalf("SendSystem: %v", err)
	}
	// A resolvable message, left untouched, to prove the batch only
	// deletes what was asked for.
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	bob, err := users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	kept, err := srv.Netmail.Send(alice.ID, "", bob.ID, "bob", "", "Hi", "body", false)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodPost, "/api/netmail/unresolved/batch-delete",
		map[string]any{"ids": []int64{a.ID, b.ID}}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Deleted int `json:"deleted"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Deleted != 2 {
		t.Fatalf("deleted = %d, want 2", resp.Deleted)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/netmail/unresolved", nil, token)
	var msgs []unresolvedNetmailSummaryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &msgs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("msgs after batch delete = %+v, want empty", msgs)
	}

	if _, err := srv.Netmail.MessageByID(kept.ID); err != nil {
		t.Fatalf("MessageByID(kept): %v, want the untargeted message to survive", err)
	}
}

func TestBatchDeleteUnresolvedNetmailRejectsEmptyIDs(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodPost, "/api/netmail/unresolved/batch-delete", map[string]any{"ids": []int64{}}, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetUnresolvedNetmailRejectsAnOrdinaryMessageID(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	bob, err := users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	ordinary, err := srv.Netmail.Send(alice.ID, "", bob.ID, "bob", "", "Hi", "body", false)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodGet, fmt.Sprintf("/api/netmail/unresolved/%d", ordinary.ID), nil, token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET status = %d, want 404 -- this message resolved to a real recipient, it's not unresolved", rec.Code)
	}
}
