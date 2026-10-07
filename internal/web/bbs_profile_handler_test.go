package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/user"
)

func TestBBSProfileGetAndUpdate(t *testing.T) {
	srv, users, _ := newTestServer(t)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := users.SetRealName(alice.ID, "Alice Example"); err != nil {
		t.Fatalf("SetRealName: %v", err)
	}
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	rec := doJSON(t, h, http.MethodGet, "/api/bbs/profile", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var p profileDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Username != "alice" || p.RealName != "Alice Example" || p.Timezone != "" || p.TotalCalls != 1 {
		t.Fatalf("profile = %+v", p)
	}

	// Invalid input is rejected as a whole: the valid real name in the
	// same request must not be saved either.
	for _, body := range []map[string]any{
		{"real_name": "New Name", "timezone": "Mars/Olympus"},
		{"real_name": "  "},
		{"real_name": "Sysop"},
		{"timezone": "Local"},
	} {
		rec = doJSON(t, h, http.MethodPut, "/api/bbs/profile", body, token)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("PUT %v status = %d, want 400", body, rec.Code)
		}
	}
	stored, _ := users.ByID(alice.ID)
	if stored.RealName != "Alice Example" || stored.Timezone != "" {
		t.Fatalf("rejected update changed the account: %+v", stored)
	}

	// Timezone only; real name untouched.
	rec = doJSON(t, h, http.MethodPut, "/api/bbs/profile", map[string]any{"timezone": "Europe/Zurich"}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Timezone != "Europe/Zurich" || p.RealName != "Alice Example" {
		t.Fatalf("after timezone update: %+v", p)
	}

	// Both, then unset the timezone again.
	rec = doJSON(t, h, http.MethodPut, "/api/bbs/profile", map[string]any{"real_name": " Alice Changed ", "timezone": ""}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body=%s", rec.Code, rec.Body.String())
	}
	stored, _ = users.ByID(alice.ID)
	if stored.RealName != "Alice Changed" || stored.Timezone != "" {
		t.Fatalf("after combined update: %+v", stored)
	}
}

func TestBBSLoginReturnsTimezone(t *testing.T) {
	srv, users, _ := newTestServer(t)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := users.SetTimezone(alice.ID, "Asia/Tokyo"); err != nil {
		t.Fatalf("SetTimezone: %v", err)
	}
	rec := doJSON(t, srv.Routes(), http.MethodPost, "/api/bbs/auth/login", map[string]string{
		"username": "alice", "password": "password123",
	}, "")
	var resp struct {
		Timezone string `json:"timezone"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Timezone != "Asia/Tokyo" {
		t.Fatalf("login timezone = %q, want Asia/Tokyo", resp.Timezone)
	}
}

func TestBBSChangePassword(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	cases := []struct {
		current, next string
		want          int
	}{
		{"wrong", "newpass1", http.StatusBadRequest},
		{"password123", "abc", http.StatusBadRequest},
		{"password123", "newpass1", http.StatusNoContent},
	}
	for _, c := range cases {
		rec := doJSON(t, h, http.MethodPost, "/api/bbs/profile/password", map[string]string{
			"current_password": c.current, "new_password": c.next,
		}, token)
		if rec.Code != c.want {
			t.Fatalf("change %q->%q status = %d, want %d (body=%s)", c.current, c.next, rec.Code, c.want, rec.Body.String())
		}
	}
	if _, err := users.Authenticate("alice", "newpass1"); err != nil {
		t.Fatalf("login with new password: %v", err)
	}
}

func TestBBSProfileQWKRouting(t *testing.T) {
	srv, users, _ := newTestServer(t)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	rec := doJSON(t, h, http.MethodPut, "/api/bbs/profile", map[string]any{"qwk_routing": true}, token)
	var p profileDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || !p.QWKRouting {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body.String())
	}
	stored, _ := users.ByID(alice.ID)
	if !stored.QWKRouting {
		t.Fatal("not stored")
	}
	// Leaving the field out leaves it alone.
	doJSON(t, h, http.MethodPut, "/api/bbs/profile", map[string]any{"timezone": "UTC"}, token)
	if stored, _ = users.ByID(alice.ID); !stored.QWKRouting {
		t.Fatal("an unrelated update turned it off")
	}
}

func TestBBSProfileLocation(t *testing.T) {
	srv, users, _ := newTestServer(t)
	users.Register("alice", "password123", user.SLNewUser)
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	rec := doJSON(t, h, http.MethodPut, "/api/bbs/profile", map[string]string{"location": "  Neunkirch,   Switzerland "}, token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"location":"Neunkirch, Switzerland"`) {
		t.Fatalf("set location: %d %s", rec.Code, rec.Body.String())
	}
	long := strings.Repeat("x", 41)
	if rec := doJSON(t, h, http.MethodPut, "/api/bbs/profile", map[string]string{"location": long}, token); rec.Code != http.StatusBadRequest {
		t.Fatalf("41 characters: status %d, want 400", rec.Code)
	}
	u, _ := users.ByUsername("alice")
	if u.Place != "Neunkirch, Switzerland" {
		t.Fatalf("stored place = %q", u.Place)
	}
}
