package web

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/midrei/nullmodem-bbs/internal/user"
)

func TestAdminLoginWithTwoFactor(t *testing.T) {
	srv, users, _ := newTestServer(t)
	h := srv.Routes()
	maik, _ := users.Register("maik", "password123", user.SLNewUser) // sysop
	other, _ := users.Register("other", "password123", user.SLNewUser)
	users.SetSecurityLevel(other.ID, user.SLSysop)
	token := loginAsSysop(t, h, "maik", "password123")

	// Set up through the API, as the Security page does.
	rec := doJSON(t, h, http.MethodPost, "/api/account/totp/start", nil, token)
	var setup user.TOTPSetup
	json.Unmarshal(rec.Body.Bytes(), &setup)
	if rec.Code != http.StatusOK || setup.Secret == "" {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	code, _ := totp.GenerateCode(setup.Secret, time.Now())
	rec = doJSON(t, h, http.MethodPost, "/api/account/totp/confirm", map[string]string{"code": code}, token)
	var conf struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	json.Unmarshal(rec.Body.Bytes(), &conf)
	if rec.Code != http.StatusOK || len(conf.RecoveryCodes) != 8 {
		t.Fatalf("confirm: %d %s", rec.Code, rec.Body.String())
	}

	login := func(code string) (int, map[string]any) {
		rec := doJSON(t, h, http.MethodPost, "/api/auth/login", map[string]string{"username": "maik", "password": "password123", "code": code}, "")
		var body map[string]any
		json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body
	}
	if c, body := login(""); c != http.StatusUnauthorized || body["totp_required"] != true {
		t.Fatalf("no code: %d %v", c, body)
	}
	if c, _ := login("000000"); c != http.StatusUnauthorized {
		t.Fatalf("wrong code: %d", c)
	}
	if c, body := login(conf.RecoveryCodes[0]); c != http.StatusOK || body["token"] == nil {
		t.Fatalf("recovery code: %d %v", c, body)
	}

	// Required for the admin: the other sysop (no two-factor) is out.
	rec = doJSON(t, h, http.MethodGet, "/api/security", nil, token)
	var sec securityResponse
	json.Unmarshal(rec.Body.Bytes(), &sec)
	sec.Settings.RequireAdminTOTP = true
	if rec = doJSON(t, h, http.MethodPut, "/api/security/settings", sec.Settings, token); rec.Code != http.StatusOK {
		t.Fatalf("require: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, http.MethodPost, "/api/auth/login", map[string]string{"username": "other", "password": "password123"}, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("sysop without two-factor got in: %d", rec.Code)
	}
	// Setting another account's password (a caller who forgot theirs).
	if rec = doJSON(t, h, http.MethodPut, "/api/users/"+itoa(other.ID)+"/password", map[string]string{"password": "fresh password"}, token); rec.Code != http.StatusNoContent {
		t.Fatalf("set password: %d", rec.Code)
	}
	if _, err := users.Authenticate("other", "fresh password"); err != nil {
		t.Fatal("new password not set")
	}
	_ = maik
}
