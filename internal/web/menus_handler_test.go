package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/user"
)

func TestListMenusReturnsSeededMenu(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodGet, "/api/menus", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET /api/menus status = %d, want 401", rec.Code)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/menus", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/menus status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var menus []menuDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &menus); err != nil {
		t.Fatalf("decode menus: %v", err)
	}
	if len(menus) != 1 || menus[0].Name != "main" || len(menus[0].Items) != 2 {
		t.Fatalf("menus = %+v, want one seeded 'main' menu with 2 items", menus)
	}
}
