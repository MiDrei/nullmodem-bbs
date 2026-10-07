package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/services"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

func servicesByName(t *testing.T, h http.Handler, token string) map[string]serviceDTO {
	t.Helper()
	rec := doJSON(t, h, http.MethodGet, "/api/services", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/services: %d %s", rec.Code, rec.Body.String())
	}
	var list []serviceDTO
	json.Unmarshal(rec.Body.Bytes(), &list)
	out := map[string]serviceDTO{}
	for _, s := range list {
		out[s.Name] = s
	}
	return out
}

func TestServicesListRestartAndConfigMarks(t *testing.T) {
	srv, users, _ := newTestServer(t)
	users.Register("root", "supersecret", user.SLSysop)
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	if _, err := srv.Services.Register(services.Mailer, "1.0"); err != nil {
		t.Fatal(err)
	}
	got := servicesByName(t, h, token)
	if len(got) != 3 || !got["mailer"].Running || got["bbs"].Running || got["mailer"].Version != "1.0" {
		t.Fatalf("services = %+v", got)
	}

	// A BinkP change marks the mailer only.
	cfg := configDTO{Name: "X", Sysop: "root", NewUserSL: 10, TelnetEnabled: true, TelnetAddr: ":2323",
		BinkpUplinks: []binkpUplinkDTO{{Address: "21:3/100", Host: "hub:24554"}}}
	if rec := doJSON(t, h, http.MethodPut, "/api/config", cfg, token); rec.Code != http.StatusOK {
		t.Fatalf("PUT config: %d %s", rec.Code, rec.Body.String())
	}
	got = servicesByName(t, h, token)
	if n := got["mailer"].RestartNeeded; len(n) == 0 || n[len(n)-1] != "BinkP settings changed" {
		t.Fatalf("mailer restart_needed = %q", n)
	}
	for _, r := range got["web"].RestartNeeded {
		if r == "BinkP settings changed" {
			t.Fatal("a BinkP change marked the web daemon")
		}
	}

	if rec := doJSON(t, h, http.MethodPost, "/api/services/mailer/restart", map[string]string{"mode": "now"}, token); rec.Code != http.StatusAccepted {
		t.Fatalf("restart: %d", rec.Code)
	}
	if !servicesByName(t, h, token)["mailer"].RestartPending {
		t.Fatal("restart not pending after the request")
	}
	if rec := doJSON(t, h, http.MethodPost, "/api/services/mailer/restart", map[string]string{"mode": "idle"}, token); rec.Code != http.StatusBadRequest {
		t.Fatalf("idle mode for the mailer: %d, want 400", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodPost, "/api/services/nope/restart", nil, token); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown service: %d, want 404", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodGet, "/api/services", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: %d, want 401", rec.Code)
	}
}
