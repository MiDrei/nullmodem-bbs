package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

func TestEmailSettingsKeepPasswords(t *testing.T) {
	srv, users, configPath := newTestServer(t)
	users.Register("SwissMaik", "supersecret", user.SLSysop)
	h := srv.Routes()
	token := loginAsSysop(t, h, "SwissMaik", "supersecret")

	settings := map[string]any{"enabled": true, "domain": "@Example.CH", "min_sl": 20, "daily_limit": 10,
		"imap": map[string]any{"host": "imap.example.ch", "security": "tls", "user": "all@example.ch", "password": "imap-secret"},
		"smtp": map[string]any{"host": "smtp.example.ch", "port": 587, "security": "starttls", "user": "all@example.ch", "password": "smtp-secret"}}
	rec := doJSON(t, h, http.MethodPut, "/api/email", settings, token)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "-secret") {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	var got emailDTO
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Domain != "example.ch" || !got.IMAP.HasPassword || got.Example != "swissmaik@example.ch" || got.SMTP.Security != "starttls" {
		t.Fatalf("state %+v", got)
	}
	// Saving again without passwords keeps them.
	settings["imap"].(map[string]any)["password"] = ""
	delete(settings["smtp"].(map[string]any), "password")
	doJSON(t, h, http.MethodPut, "/api/email", settings, token)
	c, _ := config.Load(configPath)
	if c.Email.IMAP.Password != "imap-secret" || c.Email.SMTP.Password != "smtp-secret" || c.Email.Limit() != 10 || c.Email.IMAP.Security != "" ||
		!c.Email.Receive.Greylisting() || !c.Email.Receive.CheckSPF() {
		t.Fatalf("config %+v", c.Email)
	}
	// On without servers: refused.
	if rec := doJSON(t, h, http.MethodPut, "/api/email", map[string]any{"enabled": true, "domain": "example.ch", "daily_limit": 5}, token); rec.Code != http.StatusBadRequest {
		t.Errorf("on without servers: %d", rec.Code)
	}
}

func TestPortalWritesAndAnswersEmail(t *testing.T) {
	srv, users, configPath := newTestServer(t)
	users.Register("root", "supersecret", user.SLSysop)
	alice, _ := users.Register("alice", "password123", user.SLNewUser)
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	// The gateway off: refused.
	rec := doJSON(t, h, http.MethodPost, "/api/bbs/netmail", map[string]any{"to": "joe@other.ch", "subject": "Hi", "body": "Hello"}, token)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "isn't available") {
		t.Fatalf("gateway off: %d %s", rec.Code, rec.Body)
	}
	c, _ := config.Load(configPath)
	c.Email = config.EmailConfig{Enabled: true, Domain: "example.ch"}
	config.Save(configPath, c)

	rec = doJSON(t, h, http.MethodPost, "/api/bbs/netmail", map[string]any{"to": "joe@other.ch", "subject": "Grüezi", "body": "Hello"}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("send: %d %s", rec.Code, rec.Body)
	}
	in, err := srv.Netmail.ReceiveEmail("Joe", "joe@other.ch", alice.ID, "alice", "Re: Gr\x81ezi", "Fine!", time.Now(), "<a1@other.ch>", "")
	if err != nil {
		t.Fatal(err)
	}
	// Reading it shows the address; the reply (to the name) goes back by email.
	rec = doJSON(t, h, http.MethodGet, "/api/bbs/netmail/"+itoa(in.ID), nil, token)
	if !strings.Contains(rec.Body.String(), `"email":"joe@other.ch"`) {
		t.Fatalf("read: %s", rec.Body)
	}
	rec = doJSON(t, h, http.MethodPost, "/api/bbs/netmail", map[string]any{"to": "Joe", "subject": "Re: Re: Grüezi", "body": "Good.", "reply_to": in.ID}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("reply: %d %s", rec.Code, rec.Body)
	}
	due, _ := srv.Netmail.PendingEmail(time.Now())
	if len(due) != 2 || due[1].Email != "joe@other.ch" || due[1].InReplyTo != "<a1@other.ch>" {
		t.Fatalf("pending %+v", due)
	}
	// The profile shows the caller's address.
	rec = doJSON(t, h, http.MethodGet, "/api/bbs/profile", nil, token)
	if !strings.Contains(rec.Body.String(), `"email":"alice@example.ch"`) {
		t.Errorf("profile: %s", rec.Body)
	}
}
