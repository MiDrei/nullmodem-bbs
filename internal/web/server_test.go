package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/db"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

func newTestServer(t *testing.T) (*Server, *user.Store, string) {
	t.Helper()
	dir := t.TempDir()

	sqlDB, err := db.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	users := user.NewStore(sqlDB)

	configPath := filepath.Join(dir, "bbs.yaml")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	srv := &Server{
		Users:         users,
		BBSConfigPath: configPath,
		JWTSecret:     []byte("test-secret"),
	}
	return srv, users, configPath
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestLoginRejectsNonSysop(t *testing.T) {
	srv, users, _ := newTestServer(t)
	// Register a bootstrap sysop first so "regular" (registered second)
	// isn't auto-promoted by the first-user-becomes-sysop rule.
	if _, err := users.Register("bootstrap-sysop", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register bootstrap sysop: %v", err)
	}
	if _, err := users.Register("regular", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()

	rec := doJSON(t, h, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "regular", "password": "password123",
	}, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestLoginAndConfigRoundTrip(t *testing.T) {
	srv, users, configPath := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()

	rec := doJSON(t, h, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "root", "password": "supersecret",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var loginResp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if loginResp.Token == "" {
		t.Fatal("expected non-empty token")
	}

	// Unauthenticated request must be rejected.
	rec = doJSON(t, h, http.MethodGet, "/api/config", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET /api/config status = %d, want 401", rec.Code)
	}

	// Authenticated GET returns the current config.
	rec = doJSON(t, h, http.MethodGet, "/api/config", nil, loginResp.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/config status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got configDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if got.Name != "NullModem BBS" {
		t.Fatalf("Name = %q, want default", got.Name)
	}

	// Update and verify it persisted to disk.
	got.Name = "My Awesome BBS"
	got.NewUserSL = 20
	rec = doJSON(t, h, http.MethodPut, "/api/config", got, loginResp.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config status = %d, body=%s", rec.Code, rec.Body.String())
	}

	saved, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if saved.BBS.Name != "My Awesome BBS" || saved.BBS.NewUserSL != 20 {
		t.Fatalf("saved config = %+v, want updated name/SL", saved.BBS)
	}
}

func TestPutConfigRejectsInvalidInput(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()

	rec := doJSON(t, h, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "root", "password": "supersecret",
	}, "")
	var loginResp struct {
		Token string `json:"token"`
	}
	json.Unmarshal(rec.Body.Bytes(), &loginResp)

	bad := configDTO{Name: "", Sysop: "root", NewUserSL: 10, TelnetEnabled: true, TelnetAddr: ":2323"}
	rec = doJSON(t, h, http.MethodPut, "/api/config", bad, loginResp.Token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for empty name", rec.Code)
	}

	bad = configDTO{Name: "X", Sysop: "root", NewUserSL: 300, TelnetEnabled: true, TelnetAddr: ":2323"}
	rec = doJSON(t, h, http.MethodPut, "/api/config", bad, loginResp.Token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for out-of-range SL", rec.Code)
	}
}

func TestStaticDirNotFoundSkipsFileServer(t *testing.T) {
	srv, _, _ := newTestServer(t)
	srv.StaticDir = filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := os.Stat(srv.StaticDir); err == nil {
		t.Fatal("expected static dir to not exist")
	}
	// Routes() must not panic when the static dir is absent.
	_ = srv.Routes()
}
