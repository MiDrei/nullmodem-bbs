package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/emailgw"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

func TestNetmailForwardAPI(t *testing.T) {
	srv, users, cfgPath := newTestServer(t)
	alice, _ := users.Register("alice", "password123", 50)
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	// The gateway off: not offered.
	if rec := doJSON(t, h, http.MethodPost, "/api/bbs/profile/forward", map[string]any{"address": "a@home.example"}, token); rec.Code != http.StatusBadRequest {
		t.Fatalf("gateway off: %d %s", rec.Code, rec.Body)
	}
	c, _ := config.Load(cfgPath)
	c.Email = config.EmailConfig{Enabled: true, Domain: "bbs.example.ch", SMTP: config.MailServer{Host: "127.0.0.1", Port: 1, Security: "none"}}
	config.Save(cfgPath, c)
	srv.EmailGateway = &emailgw.Gateway{DB: srv.DB, Netmail: srv.Netmail, Users: users,
		Config: func() config.EmailConfig { c, _ := config.Load(cfgPath); return c.Email },
		Lang:   func(*user.User) string { return "en" }, BBSName: func() string { return "Test BBS" }}

	// The board's own domain is refused; a server that can't be
	// reached says so.
	if rec := doJSON(t, h, http.MethodPost, "/api/bbs/profile/forward", map[string]any{"address": "x@bbs.example.ch"}, token); rec.Code != http.StatusBadRequest {
		t.Errorf("own domain: %d", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodPost, "/api/bbs/profile/forward", map[string]any{"address": "a@home.example"}, token); rec.Code != http.StatusBadGateway {
		t.Errorf("no SMTP server: %d", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodPost, "/api/bbs/profile/forward/confirm", map[string]any{"code": "123456"}, token); rec.Code != http.StatusBadRequest {
		t.Errorf("code without one asked for: %d", rec.Code)
	}

	// A confirmed forwarding shows in the profile; mark read; off.
	srv.DB.Exec(`INSERT INTO netmail_forward (user_id, address, verified) VALUES (?, 'a@home.example', 1)`, alice.ID)
	rec := doJSON(t, h, http.MethodPut, "/api/bbs/profile/forward", map[string]any{"mark_read": true}, token)
	var p profileDTO
	json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != http.StatusOK || p.Forward == nil || !p.Forward.Verified || !p.Forward.MarkRead || p.Forward.Address != "a@home.example" {
		t.Fatalf("mark read: %d %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, h, http.MethodDelete, "/api/bbs/profile/forward", nil, token)
	p = profileDTO{}
	json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != http.StatusOK || p.Forward != nil {
		t.Errorf("off: %d %s", rec.Code, rec.Body)
	}
}
