package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/user"
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

func TestSetMenuItemSLPersistsToDisk(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodPut, "/api/menus/main/items/W", map[string]int{"min_sl": 50}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT menu item status = %d, body=%s", rec.Code, rec.Body.String())
	}

	// Reload from a fresh handler call to confirm it was actually
	// persisted to disk, not just mutated in memory.
	rec = doJSON(t, h, http.MethodGet, "/api/menus", nil, token)
	var menus []menuDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &menus); err != nil {
		t.Fatalf("decode menus: %v", err)
	}
	var got *menuItemDTO
	for i := range menus[0].Items {
		if menus[0].Items[i].Key == "W" {
			got = &menus[0].Items[i]
		}
	}
	if got == nil || got.MinSL != 50 {
		t.Fatalf("item W = %+v, want min_sl 50", got)
	}

	// Unknown menu/item.
	rec = doJSON(t, h, http.MethodPut, "/api/menus/nope/items/W", map[string]int{"min_sl": 10}, token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PUT unknown menu status = %d, want 404", rec.Code)
	}
	rec = doJSON(t, h, http.MethodPut, "/api/menus/main/items/ZZ", map[string]int{"min_sl": 10}, token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PUT unknown item status = %d, want 404", rec.Code)
	}

	// Out of range.
	rec = doJSON(t, h, http.MethodPut, "/api/menus/main/items/W", map[string]int{"min_sl": 300}, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT out-of-range min_sl status = %d, want 400", rec.Code)
	}
}
