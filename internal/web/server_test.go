package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/applog"
	"git.maik.ch/swissmaik/nullmodem/internal/archive"
	"git.maik.ch/swissmaik/nullmodem/internal/areafix"
	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/db"
	"git.maik.ch/swissmaik/nullmodem/internal/file"
	"git.maik.ch/swissmaik/nullmodem/internal/message"
	"git.maik.ch/swissmaik/nullmodem/internal/netmail"
	"git.maik.ch/swissmaik/nullmodem/internal/session"
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

	// No ClearAll here: the web daemon must never wipe the BBS
	// daemon's live session state just by starting (see ClearAll's
	// doc comment) -- these tests mirror that by only ever using
	// NewStore directly, the same as production cmd/web does.
	nodes := session.NewStore(sqlDB)

	menusDir := filepath.Join(dir, "menus")
	if err := os.MkdirAll(menusDir, 0o755); err != nil {
		t.Fatalf("MkdirAll menus: %v", err)
	}
	mainMenu := "name: main\ntitle: Main Menu\nitems:\n" +
		"  - key: W\n    label: \"Who's online\"\n    action: \"builtin:who\"\n    min_sl: 0\n" +
		"  - key: S\n    label: \"Sysop menu\"\n    action: \"goto:sysop\"\n    min_sl: 200\n"
	if err := os.WriteFile(filepath.Join(menusDir, "main.yaml"), []byte(mainMenu), 0o644); err != nil {
		t.Fatalf("write main.yaml: %v", err)
	}

	screensDir := filepath.Join(dir, "screens")
	if err := os.MkdirAll(screensDir, 0o755); err != nil {
		t.Fatalf("MkdirAll screens: %v", err)
	}
	welcomeScreen := "\x1b[1;36m{BBSNAME}\x1b[0m\r\nSysop: {SYSOP}\r\n"
	if err := os.WriteFile(filepath.Join(screensDir, "welcome.ans"), []byte(welcomeScreen), 0o644); err != nil {
		t.Fatalf("write welcome.ans: %v", err)
	}

	configPath := filepath.Join(dir, "bbs.yaml")
	initial := config.Default()
	initial.BBS.Name = "Test BBS"
	initial.BBS.MenusDir = menusDir
	initial.BBS.ScreensDir = screensDir
	if err := config.Save(configPath, initial); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	logs := applog.NewStore(sqlDB)

	srv := &Server{
		Users:         users,
		Messages:      message.NewStore(sqlDB),
		Files:         file.NewStore(sqlDB, filepath.Join(dir, "files")),
		Netmail:       netmail.NewStore(sqlDB),
		Nodes:         nodes,
		Logs:          logs,
		Logger:        applog.NewLogger(logs, "web"),
		EchoAreafix:   areafix.NewEchoStore(sqlDB),
		FileAreafix:   areafix.NewFileStore(sqlDB),
		Archive:       archive.NewStore(sqlDB, filepath.Join(dir, "archive")),
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

func loginAsSysop(t *testing.T, h http.Handler, username, password string) string {
	t.Helper()
	rec := doJSON(t, h, http.MethodPost, "/api/auth/login", map[string]string{
		"username": username, "password": password,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	return resp.Token
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
	if got.Name != "Test BBS" {
		t.Fatalf("Name = %q, want seeded fixture value", got.Name)
	}

	// Update and verify it persisted to disk.
	got.Name = "My Awesome BBS"
	got.NewUserSL = 20
	got.FTNAddresses = []string{"1:234/56.0", "2:345/67"}
	got.BinkpUplinks = []binkpUplinkDTO{
		{
			Address: "21:3/194", Host: "bbs.maik.ch:24554", Password: "secret", PollIntervalSeconds: 7200,
			PacketPassword: "pktpass", TICPassword: "ticpass", AreafixPassword: "areapass", Hold: true,
			AKAAddresses: []string{"1:234/56.0"},
		},
	}
	got.BinkpDefaultPollIntervalSeconds = 1800
	rec = doJSON(t, h, http.MethodPut, "/api/config", got, loginResp.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config status = %d, body=%s", rec.Code, rec.Body.String())
	}

	saved, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	wantAddrs := []string{"1:234/56.0", "2:345/67"}
	if saved.BBS.Name != "My Awesome BBS" || saved.BBS.NewUserSL != 20 || !reflect.DeepEqual(saved.BBS.FTNAddresses, wantAddrs) {
		t.Fatalf("saved config = %+v, want updated name/SL/ftn_addresses %v", saved.BBS, wantAddrs)
	}
	if saved.Binkp.PollIntervalSeconds != 1800 {
		t.Fatalf("saved.Binkp.PollIntervalSeconds = %d, want 1800", saved.Binkp.PollIntervalSeconds)
	}
	if len(saved.Binkp.Uplinks) != 1 || saved.Binkp.Uplinks[0].Host != "bbs.maik.ch:24554" ||
		saved.Binkp.Uplinks[0].Address != "21:3/194" || saved.Binkp.Uplinks[0].Password != "secret" ||
		saved.Binkp.Uplinks[0].PollIntervalSeconds != 7200 ||
		saved.Binkp.Uplinks[0].PacketPassword != "pktpass" ||
		saved.Binkp.Uplinks[0].TICPassword != "ticpass" ||
		saved.Binkp.Uplinks[0].AreafixPassword != "areapass" ||
		!saved.Binkp.Uplinks[0].Hold ||
		!reflect.DeepEqual(saved.Binkp.Uplinks[0].AKAAddresses, []string{"1:234/56.0"}) {
		t.Fatalf("saved.Binkp.Uplinks = %+v, want one uplink with the round-tripped fields", saved.Binkp.Uplinks)
	}
}

func TestDashboardReportsCountsAndActiveNodes(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := srv.Nodes.Join("127.0.0.1:1234", "ansi"); err != nil {
		t.Fatalf("Nodes.Join: %v", err)
	}
	h := srv.Routes()

	rec := doJSON(t, h, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "root", "password": "supersecret",
	}, "")
	var loginResp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/dashboard", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET /api/dashboard status = %d, want 401", rec.Code)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/dashboard", nil, loginResp.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/dashboard status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got dashboardDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	if got.UserCount != 2 {
		t.Fatalf("UserCount = %d, want 2", got.UserCount)
	}
	if got.MessageAreaCount != 1 || got.FileAreaCount != 1 {
		t.Fatalf("area counts = %+v, want 1 seeded message area and 1 seeded file area", got)
	}
	if got.BBSName != "Test BBS" {
		t.Fatalf("BBSName = %q, want %q", got.BBSName, "Test BBS")
	}
	if len(got.Nodes) != 1 || got.Nodes[0].RemoteIP != "127.0.0.1:1234" {
		t.Fatalf("Nodes = %+v, want one active node", got.Nodes)
	}
}

func TestDashboardReportsBinkpStatus(t *testing.T) {
	srv, users, configPath := newTestServer(t)
	root, err := users.Register("root", "supersecret", user.SLSysop)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	c, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	c.BBS.FTNAddresses = []string{"21:3/100", "954:700/1"}
	c.Binkp.Uplinks = []config.BinkpUplink{
		{Address: "21:3/100", Host: "n3.z21.example.org:24554"},
		{Address: "954:700/1", Host: "n700.z954.example.org:24554", PollDisabled: true},
		{Address: "9999:1/100", Host: "n/a", Hold: true},
	}
	if err := config.Save(configPath, c); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	if _, err := srv.Netmail.Send(root.ID, "21:3/100", 0, "Someone", "21:3/200", "Hi", "body", false); err != nil {
		t.Fatalf("Netmail.Send: %v", err)
	}
	if _, err := srv.Netmail.Send(root.ID, "21:3/100", 0, "Someone", "954:700/2", "Urgent", "body", true); err != nil {
		t.Fatalf("Netmail.Send: %v", err)
	}

	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodGet, "/api/dashboard", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/dashboard status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got dashboardDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	if got.Binkp.UplinkCount != 3 {
		t.Fatalf("Binkp.UplinkCount = %d, want 3", got.Binkp.UplinkCount)
	}
	if got.Binkp.CrashOnlyUplinkCount != 1 {
		t.Fatalf("Binkp.CrashOnlyUplinkCount = %d, want 1", got.Binkp.CrashOnlyUplinkCount)
	}
	if got.Binkp.HoldUplinkCount != 1 {
		t.Fatalf("Binkp.HoldUplinkCount = %d, want 1", got.Binkp.HoldUplinkCount)
	}
	if got.Binkp.PendingOutbound != 2 {
		t.Fatalf("Binkp.PendingOutbound = %d, want 2", got.Binkp.PendingOutbound)
	}
	if got.Binkp.PendingCrash != 1 {
		t.Fatalf("Binkp.PendingCrash = %d, want 1", got.Binkp.PendingCrash)
	}
	if len(got.Binkp.OwnFTNAddresses) != 2 || got.Binkp.OwnFTNAddresses[0] != "21:3/100" {
		t.Fatalf("Binkp.OwnFTNAddresses = %v, want [21:3/100 954:700/1]", got.Binkp.OwnFTNAddresses)
	}
}

func TestDashboardReportsPendingAreasAndUnresolvedNetmailCounts(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodGet, "/api/dashboard", nil, token)
	var got dashboardDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	if got.PendingMessageAreaCount != 0 || got.PendingFileAreaCount != 0 || got.UnresolvedNetmailCount != 0 {
		t.Fatalf("initial counts = %+v, want all zero", got)
	}

	if _, _, err := srv.Messages.EnsureArea("FSXNET_GENERAL", "FSXNET_GENERAL", "fsxNet"); err != nil {
		t.Fatalf("EnsureArea (message): %v", err)
	}
	if _, _, err := srv.Files.EnsureArea("SOME_FILE_ECHO", "SOME_FILE_ECHO", ""); err != nil {
		t.Fatalf("EnsureArea (file): %v", err)
	}
	if _, err := srv.Netmail.SendSystem("Areafix", "9999:1/1", "nobody-by-this-name", "", "Re: subscribe", "body", true); err != nil {
		t.Fatalf("SendSystem: %v", err)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/dashboard", nil, token)
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	if got.PendingMessageAreaCount != 1 {
		t.Fatalf("PendingMessageAreaCount = %d, want 1", got.PendingMessageAreaCount)
	}
	if got.PendingFileAreaCount != 1 {
		t.Fatalf("PendingFileAreaCount = %d, want 1", got.PendingFileAreaCount)
	}
	if got.UnresolvedNetmailCount != 1 {
		t.Fatalf("UnresolvedNetmailCount = %d, want 1", got.UnresolvedNetmailCount)
	}

	// The admin page's own list caps at unresolvedNetmailLimit (50) --
	// the dashboard badge must report the true total regardless.
	for i := 0; i < 55; i++ {
		if _, err := srv.Netmail.SendSystem("Areafix", "9999:1/1", "nobody-by-this-name", "", "Re: subscribe", "body", true); err != nil {
			t.Fatalf("SendSystem: %v", err)
		}
	}
	rec = doJSON(t, h, http.MethodGet, "/api/dashboard", nil, token)
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	if got.UnresolvedNetmailCount != 56 {
		t.Fatalf("UnresolvedNetmailCount = %d, want 56 (not capped at unresolvedNetmailLimit)", got.UnresolvedNetmailCount)
	}
}

func TestListAndUpdateUsers(t *testing.T) {
	srv, users, _ := newTestServer(t)
	sysop, err := users.Register("root", "supersecret", user.SLSysop)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()

	rec := doJSON(t, h, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "root", "password": "supersecret",
	}, "")
	var loginResp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/users", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET /api/users status = %d, want 401", rec.Code)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/users", nil, loginResp.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/users status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var list []userDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode users: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("GET /api/users returned %d users, want 2", len(list))
	}

	path := fmt.Sprintf("/api/users/%d", alice.ID)
	rec = doJSON(t, h, http.MethodPut, path, map[string]int{"security_level": 100}, loginResp.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT %s status = %d, body=%s", path, rec.Code, rec.Body.String())
	}
	var updated userDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated user: %v", err)
	}
	if updated.SecurityLevel != 100 {
		t.Fatalf("updated.SecurityLevel = %d, want 100", updated.SecurityLevel)
	}

	got, err := users.ByID(alice.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.SecurityLevel != 100 {
		t.Fatalf("alice's SecurityLevel in store = %d, want 100", got.SecurityLevel)
	}

	// Out of range.
	rec = doJSON(t, h, http.MethodPut, path, map[string]int{"security_level": 300}, loginResp.Token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT with out-of-range SL status = %d, want 400", rec.Code)
	}

	// Unknown user.
	rec = doJSON(t, h, http.MethodPut, "/api/users/999999", map[string]int{"security_level": 50}, loginResp.Token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PUT unknown user status = %d, want 404", rec.Code)
	}

	// Demoting the last sysop must be refused.
	sysopPath := fmt.Sprintf("/api/users/%d", sysop.ID)
	rec = doJSON(t, h, http.MethodPut, sysopPath, map[string]int{"security_level": 50}, loginResp.Token)
	if rec.Code != http.StatusConflict {
		t.Fatalf("PUT demoting last sysop status = %d, want 409, body=%s", rec.Code, rec.Body.String())
	}
}

// TestUpdateUserRealName locks in that a sysop can correct a real
// name via the same PUT the Users admin page's Save button already
// uses for security level, and that omitting real_name entirely (the
// shape TestListAndUpdateUsers' plain security_level-only PUTs use)
// never clears an existing one.
func TestUpdateUserRealName(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	path := fmt.Sprintf("/api/users/%d", alice.ID)
	rec := doJSON(t, h, http.MethodPut, path,
		map[string]any{"security_level": alice.SecurityLevel, "real_name": "Alice Example"}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var updated userDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.RealName != "Alice Example" {
		t.Fatalf("RealName = %q, want %q", updated.RealName, "Alice Example")
	}

	// A PUT that omits real_name entirely must not clear it.
	rec = doJSON(t, h, http.MethodPut, path, map[string]int{"security_level": alice.SecurityLevel}, token)
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.RealName != "Alice Example" {
		t.Fatalf("RealName after security_level-only PUT = %q, want it preserved as %q", updated.RealName, "Alice Example")
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

	bad = configDTO{
		Name: "X", Sysop: "root", NewUserSL: 10, TelnetEnabled: true, TelnetAddr: ":2323",
		BinkpDefaultPollIntervalSeconds: -1,
	}
	rec = doJSON(t, h, http.MethodPut, "/api/config", bad, loginResp.Token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for negative default poll interval", rec.Code)
	}

	bad = configDTO{
		Name: "X", Sysop: "root", NewUserSL: 10, TelnetEnabled: true, TelnetAddr: ":2323",
		BinkpUplinks: []binkpUplinkDTO{{Host: "example.org:24554", PollIntervalSeconds: -1}},
	}
	rec = doJSON(t, h, http.MethodPut, "/api/config", bad, loginResp.Token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for negative per-uplink poll interval", rec.Code)
	}

	bad = configDTO{
		Name: "X", Sysop: "root", NewUserSL: 10, TelnetEnabled: true, TelnetAddr: ":2323",
		BinkpUplinks: []binkpUplinkDTO{{Host: "example.org:24554", PacketPassword: "waytoolongforfields"}},
	}
	rec = doJSON(t, h, http.MethodPut, "/api/config", bad, loginResp.Token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a packet_password longer than 8 characters", rec.Code)
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
