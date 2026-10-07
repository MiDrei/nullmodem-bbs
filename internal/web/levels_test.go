package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/user"
)

func TestSecurityLevelsDefaultsAndOwn(t *testing.T) {
	srv, users, _ := newTestServer(t)
	users.Register("root", "supersecret", user.SLSysop)
	h := srv.Routes()
	admin := loginAsSysop(t, h, "root", "supersecret")

	var got levelsDTO
	json.Unmarshal(doJSON(t, h, http.MethodGet, "/api/security-levels", nil, admin).Body.Bytes(), &got)
	if got.Custom || len(got.Levels) < 2 || got.Levels[len(got.Levels)-1].Level != 255 {
		t.Fatalf("defaults: %+v", got)
	}

	if rec := doJSON(t, h, http.MethodPut, "/api/security-levels", map[string]any{"levels": []map[string]any{{"level": 20, "name": "A"}, {"level": 20, "name": "B"}}}, admin); rec.Code != http.StatusBadRequest {
		t.Fatalf("twice: %d", rec.Code)
	}
	rec := doJSON(t, h, http.MethodPut, "/api/security-levels", map[string]any{"levels": []map[string]any{
		{"level": 255, "name": "Sysop"}, {"level": 10, "name": "Neuer Benutzer"}, {"level": 20, "name": " Regulärer Benutzer "}}}, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	if !got.Custom || len(got.Levels) != 3 || got.Levels[0].Level != 10 || got.Levels[1].Name != "Regulärer Benutzer" {
		t.Fatalf("own: %+v", got)
	}

	// Empty: back to the board's own.
	json.Unmarshal(doJSON(t, h, http.MethodPut, "/api/security-levels", map[string]any{"levels": []any{}}, admin).Body.Bytes(), &got)
	if got.Custom {
		t.Fatalf("reset: %+v", got)
	}
}
