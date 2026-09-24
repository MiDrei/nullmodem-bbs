package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestBBSNetmailSendResolvesLocalRecipient(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := users.Register("bob", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := users.Register("carol", "password123", user.SLNewUser); err != nil {
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

	// Alice's own inbox must not see it (she's the sender, not the
	// recipient) -- but she may still open it via her own Sent list,
	// just without recipient-only powers (marking read, replying).
	rec = doJSON(t, h, http.MethodGet, "/api/bbs/netmail", nil, aliceToken)
	var aliceInbox []bbsNetmailSummaryDTO
	json.Unmarshal(rec.Body.Bytes(), &aliceInbox)
	if len(aliceInbox) != 0 {
		t.Fatalf("alice's inbox = %+v, want empty", aliceInbox)
	}
	rec = doJSON(t, h, http.MethodGet, "/api/bbs/netmail/sent", nil, aliceToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET sent status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var aliceSent []bbsNetmailSummaryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &aliceSent); err != nil {
		t.Fatalf("decode sent: %v", err)
	}
	if len(aliceSent) != 1 || aliceSent[0].Subject != "Hey Bob" {
		t.Fatalf("alice's sent = %+v, want one message", aliceSent)
	}

	getPath := fmt.Sprintf("/api/bbs/netmail/%d", inbox[0].ID)
	rec = doJSON(t, h, http.MethodGet, getPath, nil, aliceToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("alice (sender) reading her own sent netmail status = %d, want 200", rec.Code)
	}
	var aliceView bbsNetmailDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &aliceView); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if aliceView.IsRecipient {
		t.Fatalf("alice's view of her own sent netmail has IsRecipient = true, want false")
	}

	// A third party with no connection to the message must still be
	// refused even though she knows its ID.
	carolToken := loginAsBBSUser(t, h, "carol", "password123")
	rec = doJSON(t, h, http.MethodGet, getPath, nil, carolToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("carol reading bob's netmail status = %d, want 403", rec.Code)
	}

	// Bob reading it marks it read.
	rec = doJSON(t, h, http.MethodGet, getPath, nil, bobToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("bob GET netmail status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var bobView bbsNetmailDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &bobView); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bobView.IsRecipient {
		t.Fatalf("bob's view of his own inbox netmail has IsRecipient = false, want true")
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
	if sent.ToName != "21:3/100" {
		t.Fatalf("sent.ToName = %q, want it to default to the address when to_name is omitted", sent.ToName)
	}
}

func TestBBSNetmailSendUsesProvidedFTNRecipientName(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	rec := doJSON(t, h, http.MethodPost, "/api/bbs/netmail", map[string]string{
		"to": "21:3/100", "to_name": "Bob Remote", "subject": "Hi remote", "body": "test",
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var sent bbsNetmailDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &sent); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sent.ToName != "Bob Remote" {
		t.Fatalf("sent.ToName = %q, want Bob Remote", sent.ToName)
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

func TestBBSNetmailDeleteAllowsSenderOrRecipientOnly(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := users.Register("bob", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := users.Register("carol", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	aliceToken := loginAsBBSUser(t, h, "alice", "password123")
	carolToken := loginAsBBSUser(t, h, "carol", "password123")

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
	deletePath := fmt.Sprintf("/api/bbs/netmail/%d", sent.ID)

	// Carol has no connection to this message at all.
	rec = doJSON(t, h, http.MethodDelete, deletePath, nil, carolToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("carol delete status = %d, want 403", rec.Code)
	}

	// Alice, the sender (not the recipient), may delete it from her
	// own Sent list.
	rec = doJSON(t, h, http.MethodDelete, deletePath, nil, aliceToken)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("alice (sender) delete status = %d, want 204, body=%s", rec.Code, rec.Body.String())
	}

	// It's gone for good -- there's only one row, not a per-side copy.
	rec = doJSON(t, h, http.MethodGet, deletePath, nil, aliceToken)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET after delete status = %d, want 404", rec.Code)
	}
}
