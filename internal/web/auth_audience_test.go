package web

import (
	"net/http"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/user"
)

func TestPortalTokenIsNoAdminTokenAndDemotionTakesEffect(t *testing.T) {
	srv, users, _ := newTestServer(t)
	h := srv.Routes()
	maik, _ := users.Register("maik", "password123", user.SLNewUser) // sysop

	portal := loginAsBBSUser(t, h, "maik", "password123")
	if rec := doJSON(t, h, http.MethodGet, "/api/users", nil, portal); rec.Code != http.StatusUnauthorized {
		t.Fatalf("admin API took the sysop's portal token: %d", rec.Code)
	}
	admin := loginAsSysop(t, h, "maik", "password123")
	if rec := doJSON(t, h, http.MethodGet, "/api/users", nil, admin); rec.Code != http.StatusOK {
		t.Fatalf("admin token refused: %d", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodGet, "/api/bbs/polls", nil, admin); rec.Code != http.StatusUnauthorized {
		t.Fatalf("portal API took an admin token: %d", rec.Code)
	}
	other, _ := users.Register("other", "password123", user.SLNewUser)
	users.SetSecurityLevel(other.ID, user.SLSysop) // the last sysop can't be demoted
	if err := users.SetSecurityLevel(maik.ID, 10); err != nil {
		t.Fatal(err)
	}
	if rec := doJSON(t, h, http.MethodGet, "/api/users", nil, admin); rec.Code != http.StatusForbidden {
		t.Fatalf("a demoted sysop's token still works: %d", rec.Code)
	}
}
