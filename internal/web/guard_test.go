package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/guard"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestClientIPTrustsForwardedOnlyFromLoopback(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.9")
	if ip := clientIP(r); ip != "203.0.113.9" {
		t.Errorf("behind the proxy: %q, want the entry the proxy added", ip)
	}
	// Docker's port mapping: the proxy's request comes from the gateway.
	r.RemoteAddr = "172.18.0.1:40000"
	if ip := clientIP(r); ip != "203.0.113.9" {
		t.Errorf("through Docker's gateway: %q, want the forwarded address", ip)
	}
	r.RemoteAddr = "198.51.100.1:5555"
	if ip := clientIP(r); ip != "198.51.100.1" {
		t.Errorf("direct: %q, a forged header was believed", ip)
	}
}

func TestPortalLoginLockoutAndPendingAccount(t *testing.T) {
	srv, users, _ := newTestServer(t)
	srv.Guard = guard.New(users.DB(), func() guard.Settings {
		return guard.Settings{Enabled: true, MaxFailures: 2, Window: time.Minute, Lockout: time.Minute, MaxLockout: time.Hour}
	}, nil)
	h := srv.Routes()
	users.Register("maik", "password123", user.SLNewUser) // sysop
	bob, _ := users.RegisterNew("bob", "password123", 5, true)

	login := func(pw string) int {
		var buf bytes.Buffer
		json.NewEncoder(&buf).Encode(map[string]string{"username": "bob", "password": pw})
		req := httptest.NewRequest(http.MethodPost, "/api/bbs/auth/login", &buf)
		req.RemoteAddr = "127.0.0.1:1"
		req.Header.Set("X-Forwarded-For", "203.0.113.50")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if c := login("nope"); c != http.StatusUnauthorized {
		t.Fatalf("first failure: %d", c)
	}
	if c := login("nope"); c != http.StatusTooManyRequests {
		t.Fatalf("second failure: %d, want 429 (locked out)", c)
	}
	if c := login("password123"); c != http.StatusTooManyRequests {
		t.Fatalf("right password while locked out: %d", c)
	}
	srv.Guard.Unlock("203.0.113.50")

	// A waiting account logs in and reads, but can't post.
	token := loginAsBBSUser(t, h, "bob", "password123")
	general, _ := srv.Messages.AreaByTag("general")
	rec := doJSON(t, h, http.MethodPost, "/api/bbs/message-areas/"+itoa(general.ID)+"/messages",
		map[string]string{"to_name": "All", "subject": "hi", "body": "hello"}, token)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("waiting account posted: %d", rec.Code)
	}
	rec = doJSON(t, h, http.MethodPost, "/api/bbs/netmail", map[string]string{"to": "maik", "subject": "hi", "body": "let me in"}, token)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("netmail to the sysop refused: %d %s", rec.Code, rec.Body.String())
	}

	users.Approve(bob.ID, 10)
	rec = doJSON(t, h, http.MethodPost, "/api/bbs/message-areas/"+itoa(general.ID)+"/messages",
		map[string]string{"to_name": "All", "subject": "hi", "body": "hello"}, token)
	if rec.Code >= 300 {
		t.Fatalf("approved account can't post: %d", rec.Code)
	}
}
