package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

func TestLogsEndpointRequiresAuth(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()

	rec := doJSON(t, h, http.MethodGet, "/api/logs", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET /api/logs status = %d, want 401", rec.Code)
	}
}

func TestLogsEndpointReportsActivity(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()

	token := loginAsSysop(t, h, "root", "supersecret")

	// The login itself should already have produced a log entry.
	rec := doJSON(t, h, http.MethodGet, "/api/logs", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/logs status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var entries []logEntryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode logs: %v", err)
	}
	if !containsMessage(entries, "root logged into the admin UI") {
		t.Fatalf("logs = %+v, want a login entry for root", entries)
	}

	// A failed login attempt must also be recorded.
	doJSON(t, h, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "root", "password": "wrong-password",
	}, "")

	// A config update performed as root should be recorded too.
	rec = doJSON(t, h, http.MethodGet, "/api/config", nil, token)
	var cfg configDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	cfg.Name = "Renamed BBS"
	rec = doJSON(t, h, http.MethodPut, "/api/config", cfg, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, h, http.MethodGet, "/api/logs", nil, token)
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode logs: %v", err)
	}
	if !containsMessage(entries, "failed admin login attempt") {
		t.Fatalf("logs = %+v, want a failed login entry", entries)
	}
	if !containsMessage(entries, "root updated the BBS configuration") {
		t.Fatalf("logs = %+v, want a config-update entry", entries)
	}
}

func TestLogsEndpointAfterIDOnlyReturnsNewer(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodGet, "/api/logs", nil, token)
	var initial []logEntryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &initial); err != nil {
		t.Fatalf("decode logs: %v", err)
	}
	if len(initial) == 0 {
		t.Fatal("expected at least one log entry after login")
	}
	lastID := initial[len(initial)-1].ID

	rec = doJSON(t, h, http.MethodGet, "/api/logs?after_id="+strconv.FormatInt(lastID, 10), nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/logs?after_id status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var after []logEntryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &after); err != nil {
		t.Fatalf("decode logs: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("after_id=%d returned %d entries, want 0 (no new activity yet)", lastID, len(after))
	}
}

func containsMessage(entries []logEntryDTO, substr string) bool {
	for _, e := range entries {
		if strings.Contains(e.Message, substr) {
			return true
		}
	}
	return false
}
