package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

func TestBBSNetmailSendResolvesLocalRecipient(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := users.Register("bob", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	aliceToken := loginAsBBSUser(t, h, "alice", "password123")

	rec := doJSON(t, h, http.MethodPost, "/api/bbs/netmail", map[string]string{
		"to": "bob", "subject": "Hey Bob", "body": "how's it going",
	}, aliceToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var sent bbsNetmailDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &sent); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sent.ToName != "bob" || sent.ToAddress != "" {
		t.Fatalf("sent = %+v, want to_name=bob to_address empty (local delivery)", sent)
	}

	bobToken := loginAsBBSUser(t, h, "bob", "password123")
	rec = doJSON(t, h, http.MethodGet, "/api/bbs/netmail", nil, bobToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET inbox status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var inbox []bbsNetmailSummaryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &inbox); err != nil {
		t.Fatalf("decode inbox: %v", err)
	}
	if len(inbox) != 1 || inbox[0].Subject != "Hey Bob" || !inbox[0].Unread {
		t.Fatalf("bob's inbox = %+v, want one unread message", inbox)
	}

	// Alice's own inbox must not see it -- and reading someone else's
	// mail by ID must be refused even though she knows it exists.
	rec = doJSON(t, h, http.MethodGet, "/api/bbs/netmail", nil, aliceToken)
	var aliceInbox []bbsNetmailSummaryDTO
	json.Unmarshal(rec.Body.Bytes(), &aliceInbox)
	if len(aliceInbox) != 0 {
		t.Fatalf("alice's inbox = %+v, want empty", aliceInbox)
	}
	getPath := fmt.Sprintf("/api/bbs/netmail/%d", inbox[0].ID)
	rec = doJSON(t, h, http.MethodGet, getPath, nil, aliceToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("alice reading bob's netmail status = %d, want 403", rec.Code)
	}

	// Bob reading it marks it read.
	rec = doJSON(t, h, http.MethodGet, getPath, nil, bobToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("bob GET netmail status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestBBSNetmailSendResolvesFTNAddress(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	rec := doJSON(t, h, http.MethodPost, "/api/bbs/netmail", map[string]string{
		"to": "21:3/100", "subject": "Hi remote", "body": "test",
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var sent bbsNetmailDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &sent); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sent.ToAddress != "21:3/100" {
		t.Fatalf("sent.ToAddress = %q, want 21:3/100", sent.ToAddress)
	}
}

func TestBBSNetmailSendRejectsUnresolvableRecipient(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	rec := doJSON(t, h, http.MethodPost, "/api/bbs/netmail", map[string]string{
		"to": "not-a-user-or-address", "subject": "Hi", "body": "test",
	}, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}
